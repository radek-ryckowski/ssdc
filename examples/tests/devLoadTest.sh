#!/bin/bash
trap "echo The script is terminated; exit" SIGINT
go build -C ../server/ -o ../tests
go build -C ../client/ -o ../tests
mkdir -p /tmp/1
mkdir -p /tmp/2
mkdir -p /tmp/3

./server -port=":50051" --http=":8080" --peers="127.0.0.1:50052,127.0.0.1:50053"  -wal=/tmp/1 -slog=/tmp/slog1 -db=/tmp/sql1.db &
./server -port=":50052" --http=":8081" --peers="127.0.0.1:50051,127.0.0.1:50053"  -wal=/tmp/2 -slog=/tmp/slog2 -db=/tmp/sql2.db &
./server -port=":50053" --http=":8082" --peers="127.0.0.1:50051,127.0.0.1:50052"  -wal=/tmp/3 -slog=/tmp/slog3 -db=/tmp/sql3.db &
 
sleep 1

array=(127.0.0.1:50051 127.0.0.1:50052 127.0.0.1:50053)
breakNumber=10
a=0
while true
do
        value="value$a"
        key="key$a"
        # pick radom server from array
        server=$(printf "%s\n" "${array[@]}" | shuf -n 1)
        ./client -key=$key -value=$value -address=$server -set       
        a=$((a+1))
        if [ $a -eq $breakNumber ]
        then
                break
        fi
done
a=0
while true
do
        key="key$a"
        server=$(printf "%s\n" "${array[@]}" | shuf -n 1)
        ./client -key=$key -address=$server -get
        if [ $? != 0 ]
        then
                echo "Error: $?"
        fi
        a=$((a+1))
        if [ $a -eq $breakNumber ]
        then
                break
        fi
done

ps -efa | grep server | grep wal |grep -v grep | awk '{print $2}'| xargs kill -9
