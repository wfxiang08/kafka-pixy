package groupmember

import (
	"sort"
	"sync"
	"time"

	"github.com/mailgun/kafka-pixy/actor"
	"github.com/mailgun/kafka-pixy/config"
	"github.com/mailgun/kafka-pixy/none"
	"github.com/pkg/errors"
	"github.com/samuel/go-zookeeper/zk"
	"github.com/wvanbergen/kazoo-go"
)

// It is ok for an attempt to claim a partition to fail, for it might take
// some time for the current partition owner to release it. So we won't report
// first several failures to claim a partition as an error.
const safeClaimRetriesCount = 10

// T maintains a consumer group member registration in ZooKeeper, watches for
// other members to join, leave and update their subscriptions, and generates
// notifications of such changes.
type T struct {
	actDesc          *actor.Descriptor
	cfg              *config.Proxy
	group            string
	groupZNode       *kazoo.Consumergroup
	groupMemberZNode *kazoo.ConsumergroupInstance
	topics           []string
	subscriptions    map[string][]string
	topicsCh         chan []string
	subscriptionsCh  chan map[string][]string
	stopCh           chan none.T
	wg               sync.WaitGroup
}

// Spawn creates a consumer group member instance and starts its background
// goroutines.
func Spawn(parentActDesc *actor.Descriptor, group, memberID string, cfg *config.Proxy, kazooClt *kazoo.Kazoo) *T {
	groupZNode := kazooClt.Consumergroup(group)
	groupMemberZNode := groupZNode.Instance(memberID)
	actDesc := parentActDesc.NewChild("member")
	actDesc.AddLogField("kafka.group", group)
	gm := &T{
		actDesc:          actDesc,
		cfg:              cfg,
		group:            group,
		groupZNode:       groupZNode,
		groupMemberZNode: groupMemberZNode,
		topicsCh:         make(chan []string),
		subscriptionsCh:  make(chan map[string][]string),
		stopCh:           make(chan none.T),
	}
	actor.Spawn(gm.actDesc, &gm.wg, gm.run)
	return gm
}

// Topics returns a channel to receive a list of topics the member should
// subscribe to. To make the member unsubscribe from all topics either nil or
// an empty topic list can be sent.
func (gm *T) Topics() chan<- []string {
	return gm.topicsCh
}

// Subscriptions returns a channel that subscriptions will be sent whenever a
// member joins or leaves the group or when an existing member updates its
// subscription.
func (gm *T) Subscriptions() <-chan map[string][]string {
	return gm.subscriptionsCh
}

// ClaimPartition claims a topic/partition to be consumed by this member of the
// consumer group. It blocks until either succeeds or canceled by the caller. It
// returns a function that should be called to release the claim.
func (gm *T) ClaimPartition(claimerActDesc *actor.Descriptor, topic string, partition int32, cancelCh <-chan none.T) func() {
	beginAt := time.Now()
	retries := 0
	err := gm.groupMemberZNode.ClaimPartition(topic, partition)
	for err != nil {
		logEntry := claimerActDesc.Log().WithError(err)
		logFailureFn := logEntry.Infof
		if retries++; retries > safeClaimRetriesCount {
			logFailureFn = logEntry.Errorf
		}
		logFailureFn("failed to claim partition: via=%s, retries=%d, took=%s",
			gm.actDesc, retries, millisSince(beginAt))
		select {
		case <-time.After(gm.cfg.Consumer.RetryBackoff):
		case <-cancelCh:
			return func() {}
		}
		err = gm.groupMemberZNode.ClaimPartition(topic, partition)
	}
	claimerActDesc.Log().Infof("partition claimed: via=%s, retries=%d, took=%s",
		gm.actDesc, retries, millisSince(beginAt))
	return func() {
		beginAt := time.Now()
		retries := 0
		err := gm.groupMemberZNode.ReleasePartition(topic, partition)
		for err != nil && err != kazoo.ErrPartitionNotClaimed {
			logEntry := claimerActDesc.Log().WithError(err)
			logFailureFn := logEntry.Infof
			if retries++; retries > safeClaimRetriesCount {
				logFailureFn = logEntry.Errorf
			}
			logFailureFn("failed to release partition: via=%s, retries=%d, took=%s",
				gm.actDesc, retries, millisSince(beginAt))
			<-time.After(gm.cfg.Consumer.RetryBackoff)
			err = gm.groupMemberZNode.ReleasePartition(topic, partition)
		}
		claimerActDesc.Log().Infof("partition released: via=%s, retries=%d, took=%s",
			gm.actDesc, retries, millisSince(beginAt))
	}
}

// Stop signals the consumer group member to stop and blocks until its
// goroutines are over.
func (gm *T) Stop() {
	close(gm.stopCh)
	gm.wg.Wait()
}

func (gm *T) run() {
	defer close(gm.subscriptionsCh)

	// Ensure a group ZNode exist.
	err := gm.groupZNode.Create()
	for err != nil {
		gm.actDesc.Log().WithError(err).Error("failed to create a group znode")
		select {
		case <-time.After(gm.cfg.Consumer.RetryBackoff):
		case <-gm.stopCh:
			return
		}
		err = gm.groupZNode.Create()
	}

	// Ensure that the member leaves the group in ZooKeeper on stop. We retry
	// indefinitely here until ZooKeeper confirms that there is no registration.
	defer func() {
		err := gm.groupMemberZNode.Deregister()
		for err != nil && err != kazoo.ErrInstanceNotRegistered {
			gm.actDesc.Log().WithError(err).Error("failed to deregister")
			<-time.After(gm.cfg.Consumer.RetryBackoff)
			err = gm.groupMemberZNode.Deregister()
		}
	}()

	var (
		nilOrSubscriptionsCh     chan<- map[string][]string
		nilOrGroupUpdatedCh      <-chan zk.Event
		nilOrTimeoutCh           <-chan time.Time
		pendingTopics            []string
		pendingSubscriptions     map[string][]string
		shouldSubmitTopics       = false
		shouldFetchMembers       = false
		shouldFetchSubscriptions = false
		members                  []*kazoo.ConsumergroupInstance
	)
	for {
		select {
		case topics := <-gm.topicsCh:
			pendingTopics = normalizeTopics(topics)
			shouldSubmitTopics = !topicsEqual(pendingTopics, gm.topics)
		case nilOrSubscriptionsCh <- pendingSubscriptions:
			nilOrSubscriptionsCh = nil
			gm.subscriptions = pendingSubscriptions
		case <-nilOrGroupUpdatedCh:
			nilOrGroupUpdatedCh = nil
			shouldFetchMembers = true
		case <-nilOrTimeoutCh:
		case <-gm.stopCh:
			return
		}

		if shouldSubmitTopics {
			if err = gm.submitTopics(pendingTopics); err != nil {
				gm.actDesc.Log().WithError(err).Error("failed to submit topics")
				nilOrTimeoutCh = time.After(gm.cfg.Consumer.RetryBackoff)
				continue
			}
			gm.actDesc.Log().Infof("submitted: topics=%v", pendingTopics)
			shouldSubmitTopics = false
			shouldFetchMembers = true
		}

		if shouldFetchMembers {
			members, nilOrGroupUpdatedCh, err = gm.groupZNode.WatchInstances()
			if err != nil {
				gm.actDesc.Log().WithError(err).Error("failed to watch members")
				nilOrTimeoutCh = time.After(gm.cfg.Consumer.RetryBackoff)
				continue
			}
			shouldFetchMembers = false
			shouldFetchSubscriptions = true
			// To avoid unnecessary rebalancing in case of a deregister/register
			// sequences that happen when a member updates its topic subscriptions,
			// we delay subscription fetching.
			nilOrTimeoutCh = time.After(gm.cfg.Consumer.RebalanceDelay)
			continue
		}

		if shouldFetchSubscriptions {
			pendingSubscriptions, err = gm.fetchSubscriptions(members)
			if err != nil {
				gm.actDesc.Log().WithError(err).Error("failed to fetch subscriptions")
				nilOrTimeoutCh = time.After(gm.cfg.Consumer.RetryBackoff)
				continue
			}
			shouldFetchSubscriptions = false
			gm.actDesc.Log().Infof("fetched subscriptions: %v", pendingSubscriptions)
			if subscriptionsEqual(pendingSubscriptions, gm.subscriptions) {
				nilOrSubscriptionsCh = nil
				pendingSubscriptions = nil
				gm.actDesc.Log().Infof("redundant group update ignored: %v", gm.subscriptions)
				continue
			}
			nilOrSubscriptionsCh = gm.subscriptionsCh
		}
	}
}

// fetchSubscriptions retrieves registration records for the specified members
// from ZooKeeper.
//
// FIXME: It is assumed that all members of the group are registered with the
// FIXME: `static` pattern. If a member that pattern is either `white_list` or
// FIXME: `black_list` joins the group the result will be unpredictable.
func (gm *T) fetchSubscriptions(members []*kazoo.ConsumergroupInstance) (map[string][]string, error) {
	subscriptions := make(map[string][]string, len(members))
	for _, member := range members {
		var registration *kazoo.Registration
		registration, err := member.Registration()
		for err != nil {
			return nil, errors.Wrapf(err, "failed to fetch registration, member=%s", member.ID)
		}
		// Sort topics to ensure deterministic output.
		topics := make([]string, 0, len(registration.Subscription))
		for topic := range registration.Subscription {
			topics = append(topics, topic)
		}
		subscriptions[member.ID] = normalizeTopics(topics)
	}
	return subscriptions, nil
}

func (gm *T) submitTopics(topics []string) error {
	if gm.topics != nil {
		err := gm.groupMemberZNode.Deregister()
		if err != nil && err != kazoo.ErrInstanceNotRegistered {
			return errors.Wrap(err, "failed to deregister")
		}
	}
	gm.topics = nil
	err := gm.groupMemberZNode.Register(topics)
	for err != nil {
		return errors.Wrap(err, "failed to register")
	}
	gm.topics = topics
	return nil
}

func normalizeTopics(s []string) []string {
	if s == nil || len(s) == 0 {
		return nil
	}
	sort.Sort(sort.StringSlice(s))
	return s
}

func topicsEqual(lhs, rhs []string) bool {
	if len(lhs) != len(rhs) {
		return false
	}
	for i := range lhs {
		if lhs[i] != rhs[i] {
			return false
		}
	}
	return true
}

func subscriptionsEqual(lhs, rhs map[string][]string) bool {
	if len(lhs) != len(rhs) {
		return false
	}
	for member, lhsTopics := range lhs {
		if !topicsEqual(lhsTopics, rhs[member]) {
			return false
		}
	}
	return true
}

func millisSince(t time.Time) time.Duration {
	return time.Now().Sub(t) / time.Millisecond * time.Millisecond
}
