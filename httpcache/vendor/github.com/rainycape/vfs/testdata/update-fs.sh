#!/bin/sh

set -e
cd fs
zip -r ../fs.zip *
tar cvvf ../fs.tar *
tar cvvzf ../fs.tar.gz *
tar cvvjf ../fs.tar.bz2 *
cd -
