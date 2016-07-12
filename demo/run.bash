#!/bin/bash

RESTART=0

for i in 11211 11212 11213 11221 11222 11223
do
    case `echo hallo $i | nc localhost $i | grep sentence` in
	*sentence*)
	    ;;
	*)
	    RESTART=1
	    break
	    ;;
    esac
done

if [ $RESTART = 0 ]
then
    if [ "`curl http://127.0.0.1:11200/up 2> /dev/null`" != "up" ]
    then
	RESTART=1
    fi
fi

if [ $RESTART = 0 ]
then
    exit
fi

cd `dirname $0`

ln -s lock.$$ lock
if [ "`readlink lock`" != lock.$$ ]
then
    echo Getting lock failed
    exit
fi

echo Restarting alpiner
echo
cat log/alpiner.out
echo

./stop.bash

# wacht een tijd tot de poorten weer vrij zijn
sleep 60

./start.bash

rm -f lock
