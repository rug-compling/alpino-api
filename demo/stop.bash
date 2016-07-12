#!/bin/bash

E='alpiner|server_port=112(11|12|13|21|22|23)'

pkill -f -u $USER $E

sleep 2

pkill -KILL -f -u $USER $E
