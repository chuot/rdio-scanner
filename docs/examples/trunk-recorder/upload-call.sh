#!/bin/bash

# If your Trunk Recorder instance is running on the same host
# as Rdio Scanner, you can use a dirwatch to ingest audio files
# instead of relying on this script.
#
# If on the contrary you Trunk Recorder instance runs on another
# host, then this script is the way to go.
#
# Use this script with Trunk Recorder uploadScript system property
# in its config.json file.
#
# {
#   ...
#   "uploadScript": "upload-call.sh 7537"
#   ...
# }
#
# Note that you have to pass the Rdio Scanner system Id as the
# first argument. Trunk Recorder will then add the full path
# of the audio file as the second argument.

# Change this API key to the one configured in your Rdio Scanner
# instance.
apikey=b29eb8b9-9bcd-4e6e-bb4f-d244ada12736

basename="${2%.*}"

# Change the URL to match your Rdio Scanner host.
curl -sS http://127.0.0.1:3000/api/trunk-recorder-call-upload \
     -F "key=${apikey}" \
     -F "audio=@${basename}.wav;type=audio/wav" \
     -F "meta=@${basename}.json;type=application/json" \
     -F "system=${1:-0}"

# The audio file and its JSON file will then be deleted.
rm -f ${basename}.*
