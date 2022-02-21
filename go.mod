module github.com/aoscloud/aos_updatemanager

go 1.14

replace github.com/ThalesIgnite/crypto11 => github.com/aoscloud/crypto11 v1.0.3-0.20220217163524-ddd0ace39e6f

require (
	github.com/aoscloud/aos_common v0.0.0-20220218172038-8cf168776e9c
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f
	github.com/coreos/go-systemd/v22 v22.3.2
	github.com/godbus/dbus/v5 v5.0.4 // indirect
	github.com/joelnb/xenstore-go v0.2.0
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/looplab/fsm v0.3.0
	github.com/mattn/go-sqlite3 v1.14.9
	github.com/sirupsen/logrus v1.8.1
	github.com/tmc/scp v0.0.0-20170824174625-f7b48647feef
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/grpc v1.41.0
	gopkg.in/ini.v1 v1.63.2
)
