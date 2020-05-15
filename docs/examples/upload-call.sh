#!/bin/bash

basename="${2%.*}"

curl http://127.0.0.1:3000/api/trunk-recorder-call-upload \
     -F "key=b29eb8b9-9bcd-4e6e-bb4f-d244ada12736" \
     -F "audio=@${basename}.wav;type=audio/wav" \
     -F "meta=@${basename}.json;type=application/json" \
     -F "system=${1:-0}"

rm -f ${basename}.*
