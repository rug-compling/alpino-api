#!/bin/bash

killall -v -u "$USER" Alpino.bin alpiserv sicstus
sleep 2
killall -KILL -v -u "$USER" Alpino.bin alpiserv sicstus
