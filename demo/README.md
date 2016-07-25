Dit is een voorbeeld van een server die de Alpino-API implementeert.

Dit voorbeeld draait Alpino zelf op de lokale server.

Dit is niet geheel veilig voor in een productie-omgeving.

Voor een client, zie: https://github.com/rug-compling/paqu/tree/master/src/pqalpino

## start

Het script `start.bash` start een aantal versies van Alpino in
server-modus, en start de API-server `alpiner`.

De API-server is beschikbaar via de url http://127.0.0.1:11200/json

Het programma `alpiner` leest de configuratie in uit het bestand
`config.toml`.

Er worden zes versies van Alpino gestart, een voor elke combinatie van
time-out en parser. Alpino in server-modus verwerkt requests parallel.
Het maximum aantal zinnen dat tegelijk wordt geparst wordt beperkt door
het aantal workers in `alpiner`.

De snelheid van het parsen van een corpus is maximaal op het moment dat
het het enige corpus is dat geparst wordt. Wanneer meerdere corpora
tegelijk verwerkt worden krijgt elke worker steeds een zin uit een
willekeurig corpus. (De kans is gelijk per corpus, niet per zin.) In
principe is het mogelijk dat het parsen van een corpus nooit voltooid
wordt zolang er nieuwe corpora blijven bijkomen.

Wanneer je `DEBUG=1` zet in `start.bash` dan worden de logs van Alpino
opgeslagen in bestanden die niet automatisch geroteerd worden. Doe dit
dus alleen tijdelijk om te testen.

Het log van `alpiner` wordt wel automatisch geroteerd.

Als je `start.bash` aanroept met de vlag `-i` dan wordt `alpiner` in de
voorgrond gestart, en log-meldingen verschijnen direct op het scherm.

## stop

Het script `stop.bash` stopt `alpiner` en alle bijbehorende versies van
Alpino.

## run

Het script `run.bash` kan gebruikt worden vanuit `cron`. Het test of
alles nog draait. Zo niet, dan roept het `stop.bash`, wacht een tijd, en
roept dan `start.bash` aan. De wachttijd is nodig omdat Alpino de
poorten niet eerst sluit als het gekild wordt.

## voorbeeld

```sh
curl -d '{"request": "tokenize", "lines": false, "label": "weer"}
Vandaag & morgen

Kans op onweersbuien

De buien boven het noorden van het land trekken spoedig weg, waarna de
zon op veel plaatsen doorbreekt en het broeierig aanvoelt. In de loop
van vanmiddag en vanavond neemt de kans op stevige onweersbuien van het
zuidwesten uit toe. Vooral in het oosten kunnen deze lokaal fors
uitpakken met veel regen in korte tijd, wat voor wateroverlast kan
zorgen. Ook hagel en (zware) windstoten zijn mogelijk. De
maximumtemperaturen lopen uiteen van 22 graden op de Wadden tot 30
graden in het zuidoosten van het land. De wind draait naar het zuiden en
neemt toe naar matig. In de middag draait de wind in het westen naar
noordwest en neemt af naar zwak.

Komende nacht is er nog steeds kans op enkele forse onweersbuien. De
minimumtemperaturen liggen rond de 20 graden en de wind is zwak uit
uiteenlopende richtingen.

Morgen overdag is het half tot zwaar bewolkt en kan er in het uiterste
oosten nog een enkele bui voorkomen, maar de kans op onweer is klein. De
middagtemperatuur loopt uiteen van 19 graden vlak aan zee tot 26 graden
in het oosten van het land. De wind wordt geleidelijk westelijk en is
zwak tot matig. (Bron: KNMI)
' http://127.0.0.1:11200/json
```
