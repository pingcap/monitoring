#!/bin/sh
set -e

baseDir=$1
pushImage=$2

for file in $baseDir/*
do
    if [ -d $file ]; then
        echo ${file##*/}
        docker build -t pingcap/tidb-monitor-initializer:${file##*/} $file
        if $pushImage
        then
            docker push pingcap/tidb-monitor-initializer:${file##*/}
        fi
    fi

done

echo "Done."