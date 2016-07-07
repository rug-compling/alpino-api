# Alpino API versie 0.1

Een API voor een Alpino-server.

Deze API wordt onder andere gebruikt door [PaQu](https://github.com/rug-compling/paqu).

## Status

Deze API is nog in ontwikkeling. Meedenkers zijn welkom.

### TO DO

 * Voorbeeld-implementatie toevoegen.
 * Dit vertalen in het Engels? Beoogde gebruikers begrijpen Nederlands.
 * Sommige elementen (`timeout`, `status`, `tokens`) worden in twee
   betekenissen gebruikt. Hernoemen?

## Eigenschappen van de server

Afhankelijk van de manier waarop Alpino wordt aangeroepen hebben rechte
haken in de tekst wel of geen speciale betekenis. Voor deze API geldt
dat rechte haken **geen** speciale betekenis hebben.

Alternatief voorstel:

 * Met een parse-request kan de optie `"hints":true` gebruikt worden om
   aan te geven dat rechte haken wel een speciale betekenis hebben.
   Als de server deze mogelijkheid niet ondersteunt is de reactie een
   `501 Not Implemented`.
 * In de response van een info-request is er een `"server.hints":true`
   of `"server.hints":false` om aan te geven of de server deze feature
   ondersteunt.

## Request en result

Deze API beschrijft hoe je met json via http kunt communiceren met een
server die Alpino gebruikt om tekst te parsen. 

Elke verzoek aan de server bestaat uit een json-object. Daarna kan nog
data volgen als platte tekst. Alle verzoeken dienen met methode POST te
worden gegaan.

Elk verzoek bevat een element `request` die aangeeft wat de opdracht aan
de server is. 

request | omschrijving
--------| ------------
parse   | verzoek om tekst te parsen
output  | verzoek om (een deel van) de resultaten van een parse terug te sturen
cancel  | verzoek om een parse te annuleren
info    | verzoek om informatie over de server

Elk resultaat verstuurd door de server is een json-object, met tenminste
de elementen `code` en `status`.

element | type
--------|-------
code    | number
status  | string

Wanneer er een fout is opgetreden, een code groter dan 299, dan is er
ook een element `message`, dat nadere informatie kan bevatten.

element | type
--------|-------
message | string

Er worden onderstaande codes gebruikt. Dit zijn standaard
http-statuscodes. Bij sommige fouten kan het zijn dat de server geen
json terug stuurt, maar alleen een http-statuscode in de headers.

code | status                | omschrijving
-----|-----------------------|----------------------------------
200  | OK                    | 
202  | Accepted              | na succesvolle upload van tekst
400  | Bad Request           | fout van gebruiker
403  | Forbidden             | bijvoorbeeld: ip-adres geblokkeerd vanwege misbruik
429  | Too Many Requests     | toegang geweigerd vanwege te veel teksten tegelijk
500  | Internal Server Error | er ging iets fout in de server, wat niet fout zou mogen gaan
501  | Not Implemented       | er wordt een optie gevraagd die niet is geïmplementeerd
503  | Service Unavailable   | server is overbelast, probeer later opnieuw

TODO: Moet er in de API een *back-off policy* beschreven worden voor status
`503`, of is dat aan degene die de API implementeert?

## Lijst van requests

### Request: parse

Doel: zend een tekst naar de server om te laten parsen.

Parameters, allen optioneel:

element | type   | default  | voorwaarde   | omschrijving
--------|--------|----------|--------------|------------------------
lines   | bool   | `false`  |              | true: één zin per regel; false: doorlopenede tekst
tokens  | bool   | `false`  | lines: true  | zinnen zijn getokeniseerd
labels  | bool   | `false`  | lines: true  | zinnen hebben labels
label   | string | `"doc"`  | lines: false | prefix voor labels
timeout | int    | `0`      |              | timeout in seconden voor parsen van één zin
parser  | string | `""`     |              | gebruik alternatiev parser

Wat de timeout betreft:

 * De server kan verschillende timeouts bieden. Als de exacte waarde er
   niet bij zit, wordt de dichtsbijzijnde gebruikt.
 * Waarde 0 betekent dat de server zijn default timeout moet gebruiken.

Wat de parser betreft:

 * Er is bijvoorbeeld een alternatieve parser speciaal voor vraagzinnen.
 * Een onbekende waarde geeft een `501 Not Implemented`.
 * Waarde "" betekent dat de server de standaardparser moet gebruiken.

Suggestie:

element   | type   | default         | omschrijving
----------|--------|-----------------|------------------------
maxtokens | int    | 0 (geen limiet) | skip zinnen die meer dan dit aantal tokens hebben


Voorbeeld aanroep, tekst volgt na json-object:

```json
{
    "request": "parse",
    "lines": true,
    "tokens": true,
    "labels": true
}
doc.1.p.1.s.1|Ik besta .
doc.1.p.1.s.2|Jij bestaat .
```

Bij succes krijg je deze elementen terug:

element | type   |  omschrijving
--------|--------|--------------
code    | int    | `202`
status  | string | `Accepted`
id      | string | id van job
lines   | int    | aantal zinnen, eventueel na splitsen van lopende tekst in zinnen
timeout | int    | tijd in seconden waarbinnen output opgevraagd moet worden voordat job wordt gecanceld

De waarde van `timeout` is bij benadering. Als je ietsje over de tijd
heen zit voordat je uitvoer opvraagd, dan is er niets aan de hand, maar
als je fors over de tijd heen gaat, dan wordt de job op de server gecanceld.

Je mag ook eerder resultaten opvragen, bijvoorbeeld als je maar een of
twee zinnen laat parsen. Een goede strategie is om de eerste batch snel
op te vragen, en de wachttijd voor elke volgende batch te verlengen tot
je aan de timeout zit.

Voorbeeld uitvoer:

```json
{
    "code": 202,
    "status": "Accepted",
    "id": "118587257602604880",
    "lines": 2,
    "timeout": 300
}
```

### Request: output

Doel: opvragen van (deel van) de uitvoer van een job, momenteel alleen
jobs van type `parse`.

Parameter, verplicht:

element | type   | omschrijving
--------|--------|-------------
id      | string | id van de job

Voorbeeld aanroep:

```json
{
    "request": "output",
    "id": "118587257602604880"
}
```

Resultaat als er geen fout is opgetreden:

element  | type   | omschrijving
---------|--------|-----------
code     | int    | `200`
status   | string | `OK`
finished | bool   | `true` als parsen van alle zinnen is voltooid
batch    | array van items | de zinnen geparst tot nu toe sinds laatste aanroep 

De zinnen in batch hoeven niet aansluitend te zijn, en de volgorde is niet
gedefinieerd.

Wanneer `finished` false is, dan dien je weer binnen de timeout de
volgende batch op te vragen.

Elementen in een item in `batch`:

element | type   | voorwaarde | omschrijving
--------|--------|------------|-------------
status  | string |            | `ok` of `fail`
lineno  | int    |            | zinnummer
label   | string | indien aanwezig | label van de zin
xml     | string | status: ok | de parse van de zin
log     | string |            | error-uitvoer van de parser, of van een andere fout

Voorbeeld uitvoer:

```json
{
    "code": 200,  
    "status": "OK",
    "finished": true,
    "batch": [
{"status":"ok","lineno":2,"label":"doc.1.p.1.s.2","xml":"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<alpino_ds version=\"1.5\">\n  <node begin=\"0\" cat=\"top\" end=\"2\" id=\"0\" rel=\"top\">\n    <node begin=\"0\" cat=\"smain\" end=\"2\" id=\"1\" rel=\"--\">\n      <node begin=\"0\" case=\"nom\" def=\"def\" end=\"1\" frame=\"pronoun(nwh,je,sg,de,nom,def)\" gen=\"de\" getal=\"ev\" id=\"2\" lcat=\"np\" lemma=\"jij\" naamval=\"nomin\" num=\"sg\" pdtype=\"pron\" per=\"je\" persoon=\"2v\" pos=\"pron\" postag=\"VNW(pers,pron,nomin,vol,2v,ev)\" pt=\"vnw\" rel=\"su\" rnum=\"sg\" root=\"jij\" sense=\"jij\" status=\"vol\" vwtype=\"pers\" wh=\"nwh\" word=\"jij\"/>\n      <node begin=\"1\" end=\"2\" frame=\"verb(hebben,sg3,intransitive)\" id=\"3\" infl=\"sg3\" lcat=\"smain\" lemma=\"bestaan\" pos=\"verb\" postag=\"WW(pv,tgw,met-t)\" pt=\"ww\" pvagr=\"met-t\" pvtijd=\"tgw\" rel=\"hd\" root=\"besta\" sc=\"intransitive\" sense=\"besta\" stype=\"declarative\" tense=\"present\" word=\"bestaat\" wvorm=\"pv\"/>\n    </node>\n  </node>\n  <sentence sentid=\"82.161.115.144\">jij bestaat</sentence>\n</alpino_ds>\n","log":""},
{"status":"ok","lineno":1,"label":"doc.1.p.1.s.1","xml":"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<alpino_ds version=\"1.5\">\n  <node begin=\"0\" cat=\"top\" end=\"2\" id=\"0\" rel=\"top\">\n    <node begin=\"0\" cat=\"smain\" end=\"2\" id=\"1\" rel=\"--\">\n      <node begin=\"0\" case=\"nom\" def=\"def\" end=\"1\" frame=\"pronoun(nwh,fir,sg,de,nom,def)\" gen=\"de\" getal=\"ev\" id=\"2\" lcat=\"np\" lemma=\"ik\" naamval=\"nomin\" num=\"sg\" pdtype=\"pron\" per=\"fir\" persoon=\"1\" pos=\"pron\" postag=\"VNW(pers,pron,nomin,vol,1,ev)\" pt=\"vnw\" rel=\"su\" rnum=\"sg\" root=\"ik\" sense=\"ik\" status=\"vol\" vwtype=\"pers\" wh=\"nwh\" word=\"ik\"/>\n      <node begin=\"1\" end=\"2\" frame=\"verb(hebben,sg1,intransitive)\" id=\"3\" infl=\"sg1\" lcat=\"smain\" lemma=\"bestaan\" pos=\"verb\" postag=\"WW(pv,tgw,ev)\" pt=\"ww\" pvagr=\"ev\" pvtijd=\"tgw\" rel=\"hd\" root=\"besta\" sc=\"intransitive\" sense=\"besta\" stype=\"declarative\" tense=\"present\" word=\"besta\" wvorm=\"pv\"/>\n    </node>\n  </node>\n  <sentence sentid=\"82.161.115.144\">ik besta</sentence>\n</alpino_ds>\n","log":""}
    ]
}
```

### Request: cancel

Doel: een lopende job afbreken.

Jobs worden ook afgebroken als de timeout is verstreken.

Parameter, verplicht:

element | type   | omschrijving
--------|--------|-------------
id      | string | id van job

Voorbeeld aanroep:

```json
{
    "request": "cancel",
    "id": "118587257602604880"
}
```

Voorbeeld uitvoer:

```json
{
    "code": 200,  
    "status": "OK"
}
```

### Request: info

Doel: details over de huidige status van de server opvragen.

Geen parameters

Voorbeeld aanroep:

```json
{
    "request": "info"
}
```

Resultaat:

element  | type     |           | omschrijving
---------|----------|-----------|------------------
api      | object   |           | API-versie gebruikt door deze server
— major    | int    |           | major version number
— minor    | int    |           | minor version number
server   | object   | optioneel | gegevens over server
— about    | string | optioneel | vrije tekst, beschrijving, contact-info, etc.
— workers  | int    | optioneel | aantal werkers op dit moment, bezig of wachtend 
— jobs     | int    | optioneel | totaal aantal jobs (parse) die op dit moment verwerkt worden
— timeout  | int    | optioneel | default timeout in seconden voor parsen van één zin
— timeouts | [ int ... ]    | optioneel | ondersteunde timeouts voor parsen van één zin
— parsers  | [ string ... ] | optioneel | lijst met alternatieve parsers
limits   | object   |           | regels voor de gebruiker
— jobs     | int    |           | maximum aantal gelijktijdige jobs per IP-adres
— tokens   | int    |           | maximum lengte van een zin in tokens, 0 is geen limiet

Voorbeeld uitvoer:

```json
{
    "code": 200,  
    "status": "OK",
    "api": {
        "major": 0,
        "minor": 1
    },
    "server": {
        "about": "Experimentele server om de API te testen.\nNiet voor productiedoeleinden.\nContact: Peter Kleiweg <p.c.j.kleiweg@rug.nl>",
        "workers": 10,
        "jobs": 45,
		"timeout": 60,
		"timeouts": [ 20, 60, 180, 600 ],
		"parsers": [ "qa" ]
    },
    "limits": {
        "jobs": 6,
		"tokens": 100
    }
}
```
