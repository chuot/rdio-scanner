#!/bin/bash

api="http://127.0.0.1:3000/upload"
key="b29eb8b9-9bcd-4e6e-bb4f-d244ada12736"

basename="${2%.*}"
mp3file="$basename.mp3"
jsonfile="$basename.json"
system=$1
wavfile="$basename.wav"

exec >/dev/null 2>&1

nice -n 19 ffmpeg -i "$wavfile" -af dynaudnorm=m=20 "$mp3file" && \
curl $api \
    -F "audio=@$mp3file;type=audio/mpeg" \
    -F "json=@$jsonfile;type=application/json" \
    -F "key=$key" \
    -F "system=$system" \
&& rm $basename.*