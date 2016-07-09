#!/bin/bash

if [ -z "$ALPINO_HOME" ]
then
    echo "$0: Error: Please set your ALPINO_HOME environment variable" 1>&2
    exit 1
fi

export PROLOGMAXSIZE=800M

killall -u "$USER" Alpino.bin alpiserv
sleep 2
killall -KILL -u "$USER" Alpino.bin alpiserv
sleep 2

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=20000 \
    server_kind=parse \
    server_port=11211 \
    assume_input_is_tokenized=on \
    debug=1 \
    -init_dict_p \
    batch_command=alpino_server &> alpino11.out &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=60000 \
    server_kind=parse \
    server_port=11212 \
    assume_input_is_tokenized=on \
    debug=1 \
    -init_dict_p \
    batch_command=alpino_server &> alpino12.out &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=600000 \
    server_kind=parse \
    server_port=11213 \
    assume_input_is_tokenized=on \
    debug=1 \
    -init_dict_p \
    batch_command=alpino_server &> alpino13.out &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=20000 \
    application_type=qa \
    server_kind=parse \
    server_port=11221 \
    assume_input_is_tokenized=on \
    debug=1 \
    -init_dict_p \
    batch_command=alpino_server &> alpino21.out &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=60000 \
    application_type=qa \
    server_kind=parse \
    server_port=11222 \
    assume_input_is_tokenized=on \
    debug=1 \
    -init_dict_p \
    batch_command=alpino_server &> alpino22.out &

$ALPINO_HOME/bin/Alpino -notk -veryfast user_max=600000 \
    application_type=qa \
    server_kind=parse \
    server_port=11223 \
    assume_input_is_tokenized=on \
    debug=1 \
    -init_dict_p \
    batch_command=alpino_server &> alpino23.out &

./alpiserv &> alpiserv.out &

