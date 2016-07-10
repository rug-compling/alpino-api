#!/bin/bash

killall -v -u "$USER" Alpino.bin alpiserv
sleep 2
killall -KILL -v -u "$USER" Alpino.bin alpiserv

