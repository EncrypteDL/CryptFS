#!/bin/sh

trap 'kill $(jobs -p)' EXIT

if ! [ -d ./mnt ]; then
	mkdir intern
fi

./CryptFS meta > meta.log 2>&1 &
./CryptFS blob > blob.log 2>&1 &

sleep 1

echo "Now mount the file system with:"
echo "./dinofs mount 127.0.0.1:8000 127.0.0.1:9000 ./mnt"

tail -f ./*.log

wait