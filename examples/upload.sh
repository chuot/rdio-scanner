#!/bin/bash

api="http://127.0.0.1:3000/upload"
key="30851354-741b-4b7e-a126-4b56cca99732"

basename="${2%.*}"
jsonfile="$basename.json"
mpegfile="$basename.mp3"
system=$1
wavfile="$basename.wav"

exec >/dev/null 2>&1

nice -n 19 ffmpeg -i "$wavfile" -af dynaudnorm=m=20 "$mpegfile" && \
curl $api \
    -F "audio=@$mpegfile;type=audio/mpeg" \
    -F "json=@$jsonfile;type=application/json" \
    -F "key=$key" \
    -F "system=$system" \
&& rm $basename.*