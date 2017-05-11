#!/bin/bash

if [ -z "$ALPINO_HOME" ]
then
    echo "$0: Error: Please set your ALPINO_HOME environment variable" 1>&2
    exit 1
fi

# Are you sure you have enough memory for this?
export PROLOGMAXSIZE=8000M

DEBUG=0

if [ $DEBUG = 0 ]
then
    NULL=/dev/null
fi

mkdir -p log

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=20000 \
    server_kind=parse \
    server_port=11211 \
    assume_input_is_tokenized=on \
    debug=$DEBUG \
    -init_dict_p \
    batch_command=alpino_server &> ${NULL:-log/alpino11.out} &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=60000 \
    server_kind=parse \
    server_port=11212 \
    assume_input_is_tokenized=on \
    debug=$DEBUG \
    -init_dict_p \
    batch_command=alpino_server &> ${NULL:-log/alpino12.out} &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=600000 \
    server_kind=parse \
    server_port=11213 \
    assume_input_is_tokenized=on \
    debug=$DEBUG \
    -init_dict_p \
    batch_command=alpino_server &> ${NULL:-log/alpino13.out} &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=20000 \
    application_type=qa \
    server_kind=parse \
    server_port=11221 \
    assume_input_is_tokenized=on \
    debug=$DEBUG \
    -init_dict_p \
    batch_command=alpino_server &> ${NULL:-log/alpino21.out} &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=60000 \
    application_type=qa \
    server_kind=parse \
    server_port=11222 \
    assume_input_is_tokenized=on \
    debug=$DEBUG \
    -init_dict_p \
    batch_command=alpino_server &> ${NULL:-log/alpino22.out} &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=600000 \
    application_type=qa \
    server_kind=parse \
    server_port=11223 \
    assume_input_is_tokenized=on \
    debug=$DEBUG \
    -init_dict_p \
    batch_command=alpino_server &> ${NULL:-log/alpino23.out} &

sleep 10
for i in 11211 11212 11213 11221 11222 11223
do
    echo hallo $i | nc localhost $i | grep sentence
done

if [ "$1" = "-i" ]
then
    ./alpiner -v config.toml
    ./stop.bash
else
    ./alpiner config.toml &> log/alpiner.out &
    ps -p `pgrep -f -u $USER 'alpiner|server_port=112(11|12|13|21|22|23)'`
fi
