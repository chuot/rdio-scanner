#!/bin/bash

csv_path=/home/radio/trunk-recorder/aliases

function upload {
    basename=$(basename $2)
    csv="$2"
    name="${basename%.*}"
    system=$1

    curl -s http://localhost:3000/api/trunk-recorder-alias-upload \
         -F "csv=@$csv;type=text/csv" \
         -F "key=b29eb8b9-9bcd-4e6e-bb4f-d244ada12736" \
         -F "system=$system"
}

upload 11 "$csv_path/RSP25MTL1.csv"
upload 15 "$csv_path/RSP25MTL5.csv"
upload 21 "$csv_path/SERAM.csv"
