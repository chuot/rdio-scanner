#!/bin/bash

basename="${2%.*}"

nice -n 19 fdkaac -o "${basename}.m4a" -m3 -S "${basename}.wav"

curl -s http://127.0.0.1:3000/api/trunk-recorder-call-upload \
     -F "audio=@${basename}.m4a;type=audio/aac" \
     -F "key=b29eb8b9-9bcd-4e6e-bb4f-d244ada12736" \
     -F "meta=@${basename}.json;type=application/json" \
     -F "system=${1:-0}"

rm -f ${basename}.*
