# Changelog

#### Version 0.13.1 (TBD)

Implemented:
* Posts can now be performed with content type `x-www-form-urlencoded`, in that
  case message should be passed in the `msg` form parameter.
* Stuctural logging with sirupsen/logrus and mailgun/logrus-hooks/kafkahook.
* Support for Kafka version 0.10.2.0.

Fixed:
* [#100](https://github.com/mailgun/kafka-pixy/issues/100) Consumption from a
  partition stops if the segment that we read from expires.

#### Version 0.13.0 (2017-03-22)

Implemented:
* At-Least-Once delivery guarantee via synchronous production and
  explicit acknowledgement of consumed messages.
* Support for Kafka version 0.10.1.0.

#### Version 0.12.0 (2017-02-21)

Implemented:
* [#81](https://github.com/mailgun/kafka-pixy/pull/81) Added capability
  to proxy to multiple Kafka/ZooKeeper clusters.
* [#16](https://github.com/mailgun/kafka-pixy/issues/16) A YAML
  configuration file can be passed to Kafka-Pixy with `--config` command
  line parameter. A default configuration file is provided for reference.
* [#87](https://github.com/mailgun/kafka-pixy/pull/87) Added support for
  gRPC API.

Fixed:
* [#83](https://github.com/mailgun/kafka-pixy/issues/83) Panic in
  partition multiplexer.
* [#85](https://github.com/mailgun/kafka-pixy/pull/85) Another panic in
  partition multiplexer.

#### Version 0.11.1 (2016-08-11)

Bug fix release.
* [#64](https://github.com/mailgun/kafka-pixy/issues/64) Panic in group
  consumer.
* [#66](https://github.com/mailgun/kafka-pixy/issues/66) Group consumer
  closed while rebalancing in progress.
* [#67](https://github.com/mailgun/kafka-pixy/issues/67) Request timeout
  errors logged by offset manager.

#### Version 0.11.0 (2016-05-03)

Major overhaul and refactoring of the implementation to make it easier to
understand how the internal components interact with each other. It is an
important step before implementation of explicit acknowledgements can be
started.

During refactoring the following bugs were detected and fixed:
* [#56](https://github.com/mailgun/kafka-pixy/issues/56) Invalid stored
  offset makes consumer panic.
* [#59](https://github.com/mailgun/kafka-pixy/issues/59) Messages are
  skipped by consumer during rebalancing.
* [#62](https://github.com/mailgun/kafka-pixy/issues/62) Messages consumed
  twice during rebalancing.

#### Version 0.10.1 (2015-12-21)

* [#49](https://github.com/mailgun/kafka-pixy/pull/49) Topic consumption stops while ownership retained.

#### Version 0.10.0 (2015-12-16)

* [#47](https://github.com/mailgun/kafka-pixy/pull/47) Support for Kafka 0.9.0.0.
  Note that consumer group management is still implemented via ZooKeeper rather
  than the new Kafka [group management API](https://cwiki.apache.org/confluence/display/KAFKA/A+Guide+To+The+Kafka+Protocol#AGuideToTheKafkaProtocol-GroupMembershipAPI).

#### Version 0.9.1 (2015-11-30)

This release aims to make getting started with Kafka-Pixy easier.

* [#39](https://github.com/mailgun/kafka-pixy/issues/39) First time consume
  from a group may skip messages.
* [#40](https://github.com/mailgun/kafka-pixy/issues/40) Make output of
  `GET /topics/<>/consumers` easier to read.
* By default services listens on 19092 port for incoming API requests and a
  unix domain socket is activated only if the `--unixAddr` command line
  parameter is specified. 
* By default a pid file is not created anymore. You need to explicitly specify
  `--pidFile` command line argument to get it created.
* The source code was refactored into packages to make it easier to understand
  internal dependencies.
* Integrated with coveralls.io to collect test coverage data.

#### Version 0.8.1 (2015-10-24)

The very first version actually used in production.
