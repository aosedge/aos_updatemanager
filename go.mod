module github.com/aoscloud/aos_updatemanager

go 1.21

replace github.com/ThalesIgnite/crypto11 => github.com/aoscloud/crypto11 v1.0.3-0.20220217163524-ddd0ace39e6f

replace github.com/anexia-it/fsquota => github.com/aoscloud/fsquota v0.0.0-20231127111317-842d831105a7

require (
	github.com/aoscloud/aos_common v0.0.0-20240402105404-b14f7b83241b
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/coreos/go-systemd/v22 v22.5.0
	github.com/golang/protobuf v1.5.3
	github.com/joelnb/xenstore-go v0.3.0
	github.com/looplab/fsm v1.0.1
	github.com/mattn/go-sqlite3 v1.14.18
	github.com/sirupsen/logrus v1.9.3
	github.com/tmc/scp v0.0.0-20170824174625-f7b48647feef
	golang.org/x/crypto v0.16.0
	google.golang.org/grpc v1.59.0
	gopkg.in/ini.v1 v1.67.0
)

require (
	github.com/ThalesIgnite/crypto11 v0.0.0-00010101000000-000000000000 // indirect
	github.com/anexia-it/fsquota v0.0.0-00010101000000-000000000000 // indirect
	github.com/cavaliergopher/grab/v3 v3.0.1 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/godbus/dbus/v5 v5.0.4 // indirect
	github.com/golang-migrate/migrate/v4 v4.16.2 // indirect
	github.com/google/go-tpm v0.9.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-version v1.6.0 // indirect
	github.com/joelnb/wmi v0.0.0-20220227211458-fee931480b9c // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/lunixbochs/struc v0.0.0-20200707160740-784aaebc1d40 // indirect
	github.com/miekg/pkcs11 v1.0.3-0.20190429190417-a667d056470f // indirect
	github.com/moby/sys/mountinfo v0.7.1 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/speijnik/go-errortree v1.0.1 // indirect
	github.com/stefanberger/go-pkcs11uri v0.0.0-20230803200340-78284954bff6 // indirect
	github.com/thales-e-security/pool v0.0.2 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230822172742-b8732ec3820d // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)
