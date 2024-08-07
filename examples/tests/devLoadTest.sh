#!/bin/bash
trap "echo The script is terminated; exit" SIGINT
go build -C ../server/ -o ../tests
go build -C ../client/ -o ../tests
mkdir /tmp/1
mkdir /tmp/2
mkdir /tmp/3

./server -port=":50051" --http=":8080" --peers="127.0.0.1:50052,127.0.0.1:50053"  -wal=/tmp/1 &
./server -port=":50052" --http=":8081" --peers="127.0.0.1:50051,127.0.0.1:50053"  -wal=/tmp/2 &
./server -port=":50053" --http=":8082" --peers="127.0.0.1:50051,127.0.0.1:50052"  -wal=/tmp/3 &


array=(127.0.0.1:50051 127.0.0.1:50052 127.0.0.1:50053)
a=0
while true
do
        value="value$a"
        key="key$a"
        # pick radom server from array
        server=$(printf "%s\n" "${array[@]}" | shuf -n 1)
        echo "Server: $server"
        ./client -key=$key -value=$value -server=$server          
        a=$((a+1))
done

