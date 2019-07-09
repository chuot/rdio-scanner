#!/bin/bash

api="http://127.0.0.1:3000/upload"
key="b29eb8b9-9bcd-4e6e-bb4f-d244ada12736"

basename="${2%.*}"
aacfile="$basename.aac"
jsonfile="$basename.json"
system=$1
wavfile="$basename.wav"

exec >/dev/null 2>&1

nice -n 19 ffmpeg -i "$wavfile" -af dynaudnorm=m=20 "$aacfile" && \
curl $api \
    -F "audio=@$aacfile;type=audio/aac" \
    -F "json=@$jsonfile;type=application/json" \
    -F "key=$key" \
    -F "system=$system" \
&& rm $basename.*