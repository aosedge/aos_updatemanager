# AOS Update Manager

[![pipeline status](https://gitpct.epam.com/epmd-aepr/aos_updatemanager/badges/master/pipeline.svg)](https://gitpct.epam.com/epmd-aepr/aos_updatemanager/commits/master) 
[![coverage report](https://gitpct.epam.com/epmd-aepr/aos_updatemanager/badges/master/coverage.svg)](https://gitpct.epam.com/epmd-aepr/aos_updatemanager/commits/master)

AOS Update Manager (UM) is responsible for update of different system part. The main functions of UM:

* unpack and validate the update image
* perform update of different system part
* notify AOS Service Manager about update status

For more details see the [architecure document](doc/updatemanager.md)

# Build

## Required GO packages

All requires GO packages exist under `vendor` folder. Content of this folder is created with GO modules:

```bash
export GO111MODULE=on
export GOPRIVATE=gitpct.epam.com/*
```

```golang
go mod init
go mod vendors
```

## Native build

```
go build
```

## ARM 64 build

Install arm64 toolchain:
```
sudo apt install gcc-aarch64-linux-gnu
```
Build:

```
CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build
```

# Configuration

UM is configured through a configuration file. The file `aos_updatemanager.cfg` should be either in current directory or specified with command line option as following:
```
./aos_updatemanager -c aos_updatemanager.cfg
```
The configuration file has JSON format described [here] (doc/config.md). Example configuration file could be found in [`aos_updatemanager.cfg`](aos_updatemanager.cfg)

To increase log level use option -v:
```
./aos_updatemanager -c aos_updatemanager.cfg -v debug
```
To send log messages directly to system journal use option -j:
```
./aos_updatemanager -c aos_updatemanager.cfg -v debug -j
```
