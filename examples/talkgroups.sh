#!/bin/bash

api="http://127.0.0.1:3000/talkgroups"
key="b29eb8b9-9bcd-4e6e-bb4f-d244ada12736"

basename=$(basename $2)
csv="$2"
name="${basename%.*}"
system=$1

# exec >/dev/null 2>&1

curl $api \
    -F "key=$key" \
    -F "system=$system" \
    -F "name=$name" \
    -F "csv=@$csv;type=text/csv"