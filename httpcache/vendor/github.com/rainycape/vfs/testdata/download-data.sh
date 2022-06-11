#!/bin/sh

SRC=https://storage.googleapis.com/golang/go1.3.src.tar.gz
if which curl > /dev/null 2>&1; then
    curl -O ${SRC}
elif which wget > /dev/null 2&1; then
    wget -O `basename ${SRC}` ${SRC}
else
    echo "no curl nor wget found" 1>&2
    exit 1
fi
