# AOS Update Manager

[![CI](https://github.com/aoscloud/aos_updatemanager/workflows/CI/badge.svg)](https://github.com/aoscloud/aos_updatemanager/ctions?query=workflow%3ACI)
[![codecov](https://codecov.io/gh/aoscloud/aos_updatemanager/branch/main/graph/badge.svg?token=fvKMRCJlZv)](https://codecov.io/gh/aoscloud/aos_updatemanager)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=aoscloud_aos_updatemanager&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=aoscloud_aos_updatemanager)

Aos Update Manager (UM) is responsible for update of different system part. The main functions of UM:

* unpack and validate the update image
* perform update of different system part
* notify AOS Service Manager about update status

For more details see the [architecure document](https://docs.aoscloud.io/bin/view/Home/Architecture/Aos%20Core/Update%20Manager%20%28UM%29/)

## Build

### Required GO packages

All requires GO packages exist under `vendor` folder. Content of this folder is created with GO modules:

```bash
export GO111MODULE=on
```

```bash
go mod init
go mod vendors
```

### Native build

```bash
go build
```

## ARM 64 build

Install arm64 toolchain:

```bash
sudo apt install gcc-aarch64-linux-gnu
```

Build:

```bash
CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build
```

## Configuration

UM is configured through a configuration file. The file `aos_updatemanager.cfg` should be either in a current directory or specified with command line option as following:

```bash
./aos_updatemanager -c aos_updatemanager.cfg
```

The configuration file has JSON format described [here](https://docs.aoscloud.io/bin/view/Home/Architecture/General/Data%20formats/Core%20component%20configurations/Update%20Manager%20configuration/). Example configuration file could be found in [`aos_updatemanager.cfg`](aos_updatemanager.cfg)

To increase log level use option -v:

```bash
./aos_updatemanager -c aos_updatemanager.cfg -v debug
```

To send log messages directly to system journal use option -j:

```bash
./aos_updatemanager -c aos_updatemanager.cfg -v debug -j
```
