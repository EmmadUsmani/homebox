#!/bin/sh
set -eu
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get -o Dpkg::Options::=--force-confold upgrade -y
