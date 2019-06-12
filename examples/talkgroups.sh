#!/bin/bash

api="http://127.0.0.1:3000/talkgroups"
key="30851354-741b-4b7e-a126-4b56cca99732"

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