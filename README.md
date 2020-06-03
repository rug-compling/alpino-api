# Alpino API versie 0.93

Een API voor een [Alpino](http://www.let.rug.nl/vannoord/alp/Alpino/)-server.

Deze API wordt onder andere gebruikt door [PaQu](https://github.com/rug-compling/paqu).

Inhoud:

 * [Motivatie](#user-content-motivatie)
 * [Request en result](#user-content-request-en-result)
 * [Lijst van requests](#user-content-lijst-van-requests)
     * [parse](#user-content-request-parse)
     * [tokenize](#user-content-request-tokenize)
     * [output](#user-content-request-output)
     * [cancel](#user-content-request-cancel)
     * [info](#user-content-request-info)
 * [Speciale tekens](#user-content-specials)
     * [Commentaren](#user-content-comments)
     * [Labels](#user-content-labels)
     * [Metadata](#user-content-metadata)
     * [Instructies voor de parser](#user-content-brackets)
 * [Tokeniseren van doorlopende tekst](#user-content-partok)

## Motivatie

Er waren al wel enkele Alpino-servers, maar geen met een publieke API.
De functionaliteit van die servers is beperkt. De een geeft op een
POST-request met een tekst de geparste zinnen direct terug in de
response. Dat is alleen geschikt voor zeer kleine teksten. Een andere
server verwerkt de toegestuurde tekst geheel offline, en het resultaat
kan pas gedownload worden als de verwerking is afgerond. Beide zijn
niet geschikt voor een toepassing zoals
[PaQu](https://github.com/rug-compling/paqu).

De uitgangspunten van de huidige API zijn:

 * Een API met duidelijk beschreven opties en verwacht gedrag.
 * Geschikt voor de verwerking van kleine en zeer grote teksten, waarbij
   ook met grote teksten de resultaten vanaf het begin incrementeel
   opgevraagd kunnen worden, niet per se in de juiste volgorde.
 * Een API voor een server waarachter teksten in parallel verwerkt
   kunnen worden, door meerdere werkers, mogelijk verdeeld over meerdere
   machines. Op het moment dat er maar één tekst verwerkt wordt wordt
   het werk verdeeld over alle werkers.
 * Flexibel, toepasbaar voor meerdere soorten tekst, zoals doorlopende
   tekst, tekst die al is opgedeeld in een zin per regel, wel of niet
   getokeniseerd.

## Request en result

Deze API beschrijft hoe je met JSON via HTTP kunt communiceren met een
server die Alpino gebruikt om tekst te parsen.

Elk verzoek aan de server bestaat uit een **JSON**-object. Daarna kan
nog data volgen als platte tekst, gecodeerd in **UTF-8**. Alle verzoeken
dienen met methode **POST** te worden gegaan.

Elk verzoek bevat een element `request` dat aangeeft wat de opdracht aan
de server is.

request                                    | omschrijving
-------------------------------------------| ------------
[parse](#user-content-request-parse)       | verzoek om tekst te parsen, zo nodig eerst te tokeniseren
[tokenize](#user-content-request-tokenize) | verzoek om tekst te tokeniseren
[output](#user-content-request-output)     | verzoek om (een deel van) de resultaten van een parse terug te sturen
[cancel](#user-content-request-cancel)     | verzoek om een parse te annuleren
[info](#user-content-request-info)         | verzoek om informatie over de server

Elk resultaat verstuurd door de server is een JSON-object, type
`application/json`, met tenminste de elementen `code` en `status`.
Uitzondering: de uitvoer van `tokenize` is, als er geen fout is
opgetreden, platte tekst, type `text/plain`.

element   | type
----------|-------
`code`    | number
`status`  | string

Wanneer er een fout is opgetreden, een code groter dan 299, dan is er
ook een element `message`, dat nadere informatie kan bevatten.

element   | type
----------|-------
`message` | string

Voorbeeld uitvoer:

```json
{
    "code": 400,
    "status": "Bad Request",
    "message": "alpiner.go:336: Invalid request: pasre"
}
```

Er worden onderstaande codes gebruikt. Dit zijn standaard
HTTP-statuscodes. Bij sommige fouten (bijvoorbeeld 403, 429, 500, 503)
**kan** het zijn dat de server geen JSON terug stuurt, maar alleen een
HTTP-statuscode in de headers.

code | status                  | omschrijving
-----|-------------------------|----------------------------------
200  | `OK`                    |
202  | `Accepted`              | na succesvolle upload van tekst
400  | `Bad Request`           | fout van gebruiker
403  | `Forbidden`             | bijvoorbeeld: ip-adres geblokkeerd vanwege misbruik
405  | `Method Not Allowed`    | alleen POST is toegestaan
429  | `Too Many Requests`     | parse-request geweigerd vanwege te veel teksten tegelijk
500  | `Internal Server Error` | er ging iets fout in de server, wat niet fout zou mogen gaan
501  | `Not Implemented`       | er wordt een optie gevraagd die niet is geïmplementeerd
503  | `Service Unavailable`   | server is overbelast, probeer later opnieuw

## Lijst van requests

### Request: parse

Doel: Zend een tekst naar de server om te laten parsen. Zo nodig wordt
de tekst eerst getokeniseerd. De tekst **moet** gecodeerd zijn in
**UTF-8**, zonder BOM.

De tekst kan [speciale tekens](#user-content-specials) bevatten.

Resultaten kunnen opgevraagd worden met [Request: output](#user-content-request-output).

Parameters, allen optioneel:

element      | type   | default    | omschrijving
-------------|--------|------------|------------------------
`data_type`  | string | `text doc` | soort data: zie onder
`max_tokens` | int    | `0`        | skip zinnen die meer dan dit aantal tokens hebben
`parser`     | string | *leeg*     | gebruik alternatieve parser
`timeout`    | int    | `0`        | timeout in seconden voor parsen van één zin
`ud`         | bool   | `true`     | include Universal Dependencies

**optie:** `data_type`

Het element `data_type` geeft aan wat voor type tekst je wilt laten
verwerken. Na het type kunnen nog opties volgen, alles van elkaar
gescheiden door spaties.

type    | omschrijving
--------|--------------------------------------------------------
`text`  | de data bestaat uit doorlopende tekst
`lines` | de data bestaat uit één zin per regel

Naast de types `text` en `lines` kan een server andere types
ondersteunen. Deze types moeten vermeld zijn in `extra_types` als
resultaat van een [info-request](#user-content-request-info).

<a name="type-text"></a>
**1—** Voor type is `text`:

 * Kan gevolgd worden door een *prefix* die gebruikt wordt als begin
   van gegenereerde labels. Default is `doc`.
 * Zie [Tokeniseren van doorlopende tekst](#user-content-partok) voor details.

**2—** Voor type is `lines`:

 * De optie `tokens` geeft aan dat de zinnen al gesplitst zijn in
   tokens. Default: de zinnen zijn nog niet getokeniseerd.
 * In combinatie met de optie `tokens` kan als optie een escape-level
   meegegeven worden. Dit is een van `none`, `half` en `full`. Default
   is `half`. Zie onder bij
   [Instructies voor de parser](#user-content-brackets) voor details.

**optie:** `max_tokens`

De waarde `0` betekent geen limiet.

Als de waarde groter is dan de limiet die de server heeft ingesteld,
dan geldt de limiet van de server.

**optie:** `parser`

Er is bijvoorbeeld een alternatieve parser speciaal voor vraagzinnen.

Een onbekende waarde geeft een `400 Bad Request`.

Een lege waarde betekent dat de server de standaard-parser moet gebruiken.

**optie:** `timeout`

De server kan verschillende timeouts bieden. Als de exacte waarde er
niet bij zit, wordt de dichtstbijzijnde gebruikt.

Waarde `0` betekent dat de server zijn default timeout moet gebruiken.

**Voorbeelden aanroep**

Tekst volgt na JSON-object:

```json
{
    "request": "parse",
    "data_type": "text mijn_tekst"
}
Dit is doorlopende tekst. Zinnen lopen
door over regeleindes.
```

```json
{
    "request": "parse",
    "data_type": "lines tokens",
    "parser": "qa"
}
Hoe laat is het ?
Hoe heet jij ?
```

**Resultaat**

Bij succes krijg je deze elementen terug:

element           | type   |  omschrijving
------------------|--------|----------
`code`            | int    |`202`
`status`          | string | `Accepted`
`id`              | string | id van job
`interval`        | int    | tijd in seconden waarbinnen output opgevraagd moet worden voordat job wordt gecanceld
`timeout`         | int    | door parser gebruikte timeout in seconden per zin
`max_tokens`      | int    | door parser gebruikt maximum aantal tokens per zin
`number_of_lines` | int    | aantal zinnen, eventueel na splitsen van lopende tekst in zinnen

De waarde van `interval` is bij benadering. Als je ietsje over de tijd
heen zit voordat je [uitvoer opvraagt](#user-content-request-output),
dan is er niets aan de hand, maar als je ruim over de tijd heen gaat,
dan wordt de job op de server gecanceld.

Je mag ook eerder resultaten opvragen, bijvoorbeeld als je maar een of
twee zinnen laat parsen. Een goede strategie is om de eerste batch snel
op te vragen, en de wachttijd voor elke volgende batch te verlengen tot
je aan de waarde van `interval` zit.

Wat betreft `timeout`, `max_tokes` en `number_of_lines`: de waarde `-1`
geeft aan dat de werkelijke waarde om een of andere reden niet gegeven
kan worden.

**Voorbeeld uitvoer**

```json
{
    "code": 202,
    "status": "Accepted",
    "id": "118587257602604880",
    "interval": 300,
    "timeout": 60,
    "max_tokens": 100,
    "number_of_lines": 2
}
```

### Request: tokenize

Doel: Zend een tekst naar de server om te laten tokeniseren. De tekst
**moet** gecodeerd zijn in **UTF-8**, zonder BOM.

De tekst kan [speciale tekens](#user-content-specials) bevatten.

Parameter, optioneel:

element      | type   | default    | omschrijving
-------------|--------|------------|------------------------
`data_type`  | string | `text doc` | soort data

Zie bij [Request: parse](#user-content-request-parse) voor een
beschrijving van de waarde van `data_type`. Het enige verschil is dat
data van het type `lines` geen opties heeft.

**Voorbeeld aanroep**

Tekst volgt na JSON-object:

```json
{
    "request": "tokenize",
    "data_type": "text mijn_tekst"
}
Dit is doorlopende tekst. Zinnen lopen
door over regeleindes.
```

**Resultaat**

Bij succes krijg je platte tekst terug, type `text/plain`.

**Voorbeeld uitvoer**

```
mijn_tekst.p.1.s.1|Dit is doorlopende tekst .
mijn_tekst.p.1.s.2|Zinnen lopen door over regeleindes .
```

### Request: output

Doel: Opvragen van (deel van) de uitvoer van een
[Request: parse](#user-content-request-parse).

Parameter, **verplicht**:

element   | type   | omschrijving
----------|--------|-------------
`id`      | string | id van de job

**Voorbeeld aanroep**

```json
{
    "request": "output",
    "id": "118587257602604880"
}
```

**Resultaat**

Resultaat als er geen fout is opgetreden:

element    | type   | omschrijving
-----------|--------|-----------
`code`     | int    | `200`
`status`   | string | `OK`
`finished` | bool   | `true` als parsen van alle zinnen is voltooid
`batch`    | array van items | de zinnen geparst tot nu toe sinds laatste aanroep

De zinnen in batch hoeven niet aansluitend te zijn, en de volgorde is
niet gedefinieerd.

Wanneer `finished` false is, dan dien je weer binnen de timeout de
volgende batch op te vragen.

Elementen in een item in `batch`:

element           | type   | voorwaarde          |omschrijving
------------------|--------|---------------------|-------------
`line_status`     | string |                     | `ok`, `skipped` of `fail`
`line_number`     | int    |                     | volgnummer van de zin: eerste is nummer 1
`label`           | string | indien aanwezig     | label van de zin
`sentence`        | string |                     | de getokeniseerde zin
`alpino_ds`       | string | `line_status`: `ok` | de parse van de zin: `alpino_ds` versie 1.5 of hoger, minimaal 1.10 als met UD
`log`             | string |                     | fout-uitvoer van de parser, of van een andere fout
`parser_build`    | string | optioneel           | indien bekend, en anders dan is vermeld in de response op een `info`-request
`ud_build`        | string | optioneel           | indien bekend, en anders dan is vermeld in de response op een `info`-request
`ud_log`          | string | optioneel           | fout-uitvoer van de afleiding van Universal Dependencies

**Voorbeeld uitvoer**

```json
{
    "code": 200,
    "status": "OK",
    "batch": [
{"line_status":"ok","line_number":2,"sentence":"Hoe heet jij ?","alpino_ds":"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<alpino_ds version=\"1.5\">\n  <node begin=\"0\" cat=\"top\" end=\"4\" id=\"0\" rel=\"top\">\n    <node begin=\"0\" cat=\"whq\" end=\"3\" id=\"1\" rel=\"--\">\n      <node begin=\"0\" end=\"1\" frame=\"wh_adjective\" id=\"2\" index=\"1\" lcat=\"ap\" lemma=\"hoe\" pos=\"adj\" postag=\"BW()\" pt=\"bw\" rel=\"whd\" root=\"hoe\" sense=\"hoe\" wh=\"ywh\" word=\"Hoe\"/>\n      <node begin=\"0\" cat=\"sv1\" end=\"3\" id=\"3\" rel=\"body\">\n        <node begin=\"0\" end=\"1\" id=\"4\" index=\"1\" rel=\"predc\"/>\n        <node begin=\"1\" end=\"2\" frame=\"verb(hebben,sg,copula)\" id=\"5\" infl=\"sg\" lcat=\"sv1\" lemma=\"heten\" pos=\"verb\" postag=\"WW(pv,tgw,ev)\" pt=\"ww\" pvagr=\"ev\" pvtijd=\"tgw\" rel=\"hd\" root=\"heet\" sc=\"copula\" sense=\"heet\" stype=\"whquestion\" tense=\"present\" word=\"heet\" wvorm=\"pv\"/>\n        <node begin=\"2\" case=\"nom\" def=\"def\" end=\"3\" frame=\"pronoun(nwh,je,sg,de,nom,def)\" gen=\"de\" getal=\"ev\" id=\"6\" lcat=\"np\" lemma=\"jij\" naamval=\"nomin\" num=\"sg\" pdtype=\"pron\" per=\"je\" persoon=\"2v\" pos=\"pron\" postag=\"VNW(pers,pron,nomin,vol,2v,ev)\" pt=\"vnw\" rel=\"su\" rnum=\"sg\" root=\"jij\" sense=\"jij\" status=\"vol\" vwtype=\"pers\" wh=\"nwh\" word=\"jij\"/>\n      </node>\n    </node>\n    <node begin=\"3\" end=\"4\" frame=\"punct(vraag)\" id=\"7\" lcat=\"punct\" lemma=\"?\" pos=\"punct\" postag=\"LET()\" pt=\"let\" rel=\"--\" root=\"?\" sense=\"?\" special=\"vraag\" word=\"?\"/>\n  </node>\n  <sentence sentid=\"2\">Hoe heet jij ?</sentence>\n</alpino_ds>\n","log":""}
{"line_status":"ok","line_number":1,"sentence":"Hoe laat is het ?","alpino_ds":"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<alpino_ds version=\"1.5\">\n  <node begin=\"0\" cat=\"top\" end=\"5\" id=\"0\" rel=\"top\">\n    <node begin=\"0\" cat=\"whq\" end=\"4\" id=\"1\" rel=\"--\">\n      <node begin=\"0\" cat=\"ap\" end=\"2\" id=\"2\" index=\"1\" rel=\"whd\">\n        <node begin=\"0\" end=\"1\" frame=\"wh_adjective\" id=\"3\" lcat=\"ap\" lemma=\"hoe\" pos=\"adj\" postag=\"BW()\" pt=\"bw\" rel=\"mod\" root=\"hoe\" sense=\"hoe\" wh=\"ywh\" word=\"Hoe\"/>\n        <node aform=\"base\" begin=\"1\" buiging=\"zonder\" end=\"2\" frame=\"adjective(no_e(tmpadv))\" graad=\"basis\" id=\"4\" infl=\"no_e\" lcat=\"ap\" lemma=\"laat\" pos=\"adj\" positie=\"vrij\" postag=\"ADJ(vrij,basis,zonder)\" pt=\"adj\" rel=\"hd\" root=\"laat\" sense=\"laat\" vform=\"adj\" word=\"laat\"/>\n      </node>\n      <node begin=\"0\" cat=\"sv1\" end=\"4\" id=\"5\" rel=\"body\">\n        <node begin=\"0\" end=\"2\" id=\"6\" index=\"1\" rel=\"predc\"/>\n        <node begin=\"2\" end=\"3\" frame=\"verb(unacc,sg_heeft,copula)\" id=\"7\" infl=\"sg_heeft\" lcat=\"sv1\" lemma=\"zijn\" pos=\"verb\" postag=\"WW(pv,tgw,ev)\" pt=\"ww\" pvagr=\"ev\" pvtijd=\"tgw\" rel=\"hd\" root=\"ben\" sc=\"copula\" sense=\"ben\" stype=\"whquestion\" tense=\"present\" word=\"is\" wvorm=\"pv\"/>\n        <node begin=\"3\" end=\"4\" frame=\"determiner(het,nwh,nmod,pro,nparg,wkpro)\" genus=\"onz\" getal=\"ev\" id=\"8\" infl=\"het\" lcat=\"np\" lemma=\"het\" naamval=\"stan\" pdtype=\"pron\" persoon=\"3\" pos=\"det\" postag=\"VNW(pers,pron,stan,red,3,ev,onz)\" pt=\"vnw\" rel=\"su\" rnum=\"sg\" root=\"het\" sense=\"het\" status=\"red\" vwtype=\"pers\" wh=\"nwh\" word=\"het\"/>\n      </node>\n    </node>\n    <node begin=\"4\" end=\"5\" frame=\"punct(vraag)\" id=\"9\" lcat=\"punct\" lemma=\"?\" pos=\"punct\" postag=\"LET()\" pt=\"let\" rel=\"--\" root=\"?\" sense=\"?\" special=\"vraag\" word=\"?\"/>\n  </node>\n  <sentence sentid=\"1\">Hoe laat is het ?</sentence>\n</alpino_ds>\n","log":""},
    ],
    "finished": true
}
```

### Request: cancel

Doel: Een lopende job afbreken.

Jobs worden ook afgebroken als de timeout is verstreken.

Parameter, **verplicht**:

element   | type   | omschrijving
----------|--------|-------------
`id`      | string | id van job

**Voorbeeld aanroep**

```json
{
    "request": "cancel",
    "id": "118587257602604880"
}
```

**Voorbeeld uitvoer**

```json
{
    "code": 200,
    "status": "OK"
}
```

### Request: info

Doel: Details over de huidige status van de server opvragen.

Geen parameters

**Voorbeeld aanroep**

```json
{
    "request": "info"
}
```

**Resultaat**

element              | type           |             | omschrijving
---------------------|----------------|-------------|------------------
`code`               | int            |             | `200`
`status`             | string         |             | `OK`
`api_version`        | [ int, int ]   |             | major en minor versienummer van de API
`parser_build`       | string         | optioneel   | Alpino-versie van de parser
`tokenizer_build`    | string         | optioneel   | Alpino-versie van de tokenizer
`ud_build`           | string         | zie beneden | Indien geïmplementeerd: ID-string van de gebruikte UD-library
`about`              | string         | optioneel   | vrije tekst, beschrijving, contact-info, etc.
`workers`            | int            | optioneel   | aantal werkers op dit moment, bezig of wachtend
`total_running_jobs` | int            | optioneel   | totaal aantal jobs (parse) die op dit moment verwerkt worden
`timeout_default`    | int            | optioneel   | default timeout in seconden voor parsen van één zin
`timeout_max`        | int            | optioneel   | de maximale timeout in seconden voor parsen van één zin
`timeout_values`     | [ int ... ]    | optioneel   | ondersteunde timeouts voor parsen van één zin
`parsers`            | [ string ... ] | optioneel   | lijst met alternatieve parsers
`max_jobs`           | int            |             | maximum aantal gelijktijdige jobs per IP-adres
`max_tokens`         | int            | optioneel   | maximum lengte van een zin in tokens, 0 is geen limiet
`extra_types`        | [ string ... ] | optioneel   | extra types voor `data_type`

**Voorbeeld uitvoer**

```json
{
    "code": 200,
    "status": "OK",
    "api_version": [ 1, 0 ],
    "parser_build": "Alpino-x86_64-Linux-glibc-2.19-20973-sicstus",
    "tokenizer_build": "Alpino-x86_64-Linux-glibc-2.19-20973-sicstus",
    "ud_build": "ALUD2.4.0-alpha001",
    "about": "Experimentele server om de API te testen.\nNiet voor productiedoeleinden.\nContact: Peter Kleiweg <p.c.j.kleiweg@rug.nl>",
    "workers": 10,
    "total_running_jobs": 45,
    "timeout_default": 60,
    "timeout_max": 600,
    "timeout_values": [ 20, 60, 180, 600 ],
    "parsers": [ "qa" ],
    "max_jobs": 6,
    "maxtokens": 100,
    "extra_types": [ ]
}
```

Wat `parser_build` en `tokenizer_build` betreft:

 * Dit is de tekst uit het bestand `$ALPINO_HOME/version`.
 * Parsen en tokeniseren hoeft niet op dezelfde machine te gebeuren,
   vandaar dat ze apart worden gegeven.
 * Parsen kan op meerdere machines gebeuren, met verschillende versies
   van Alpino. Afwijkende versies kunnen vermeld worden per zin in een
   `batch`.

Wat `ud_build` betreft:

 * Niet aanwezig als Universal Dependencies niet zijn geïmplementeerd. Anders verplicht.

Wat `max_jobs` betreft:

 * Overschrijding van de limiet kan leiden tot een ban van het IP-adres
   van de client.

Wat `max_tokens` betreft:

 * De limiet kan door de client lager worden gezet, maar niet hoger.
 * Zinnen die te lang zijn resulteren in zins-status `skipped`.

## <a name="specials"></a> Speciale tekens

Tekst die je laat parsen of tokeniseren kan tekens bevatten die een
speciale betekenis hebben: `%` `|` `##META` `[` `]`

### <a name="comments"></a> Commentaren

Een regel die begint met een `%` wordt beschouwd als commentaar. Bij het
parsen wordt deze regel overgeslagen. Bij het tokeniseren wordt deze
regel ongewijzigd gekopieerd naar de uitvoer.

Een regel die begint met spaties of tabs gevolg door een `%` is **geen**
commentaar.

### <a name="labels"></a> Labels

Een `|` in de tekst wordt gebruikt om een label mee te geven aan de
zinnen. Dit verschilt per `data_type`.

**1—** Voor type is `text`:

Er worden automatisch labels toegevoegd, bestaand uit een prefix, een
paragraafnummer en een regelnummer (zie [onder](#user-content-partok)).
De standaardwaarde van prefix is `doc`, maar dat kun je veranderen door
een andere waarde mee te geven met de optie `data_type`, bijvoorbeeld:

```json
    "data_type": "text mijn_tekst"
```

Je kunt het prefix veranderen voor delen van de tekst door in de tekst
een waarde op te nemen. Een waarde bestaat uit een regel met een prefix
(zonder `|`) gevolgd door een `|` aan het eind van de regel.

Een nieuw prefix zorgt ervoor dat de telling van paragrafen opnieuw bij
1 begint, tenzij het prefix al eerder is gebruikt, want dan loopt de
telling door vanaf de laatste keer dat het prefix is gebruikt. Dus zelfs
als je meermaals hetzelfde prefix gebruikt krijgen alle zinnen een uniek
label.

Voorbeeld:

```
title|
Dit is de titel.

body|
Dit is de hoofdtekst. Deze bestaat
uit meerdere zinnen.

En meerdere paragrafen.
```

**2—** Voor type is `lines`:

Tekst die al is gesplitst in één zin per regel krijgen geen labels,
tenzij die labels al aanwezig zijn in de invoer. Een label staat vóór de
zin op dezelfde regel, gescheiden door een `|`.

Voorbeeld:

```
line.1|Dit is de eerste zin.
line.2|Dit is de tweede zin.
```

### <a name="metadata"></a> Metadata

Metadata begint met de tekst `##META`. Voorbeelden:

```
line.1|Dit is de eerste zin.
##META text dag = maandag
##META text kleur = blauw
line.2|Dit is de tweede zin.
##META text kleur = geel
##META text kleur = groen
line.3|Dit is de derde zin.
##META text dag =
line.4|Dit is de vierde zin.
```

Metadata bestaat uit een *type*, een *naam* en een *waarde*, de laatste
twee gescheiden door een is-teken. Het type is één woord. Naam en waarde
kunnen uit meerdere woorden bestaan. Type en naam kunnen geen is-teken
bevatten.

De DTD van `alpino_ds` versie 1.5 definieert de volgende waardes voor type:
`text` `int` `float` `date` `datetime`

Vanaf versie 1.11 is daar het type `bool` bijgekomen. De API dient
waardes zoals `TRUE`, `Yes`, `1`, etc, te normaliseren naar `true`,
en het tegendeel naar `false`.

Metadata wordt gedefinieerd in blokken. Een metadatablok bevat alleen
metadata, en eventueel commentaren, lege regels of labels voor
doorlopende tekst. Dus alles behalve te parsen tekst. Metadata
gedefineerd in een blok vervangt metadata met dezelfde naam uit een
eerder blok.

Bovenstaand voorbeeld leidt tot de volgend metadata per zin:

label  | metadata
-------|---------
line.1 |
line.2 | dag = maandag, kleur = blauw
line.3 | dag = maandag, kleur = geel, kleur = groen
line.4 | kleur = geel, kleur = groen

Na parsen is de metadata opgenomen in de xml. Bijvoorbeeld:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<alpino_ds version="1.5">
  <metadata>
    <meta type="text" name="dag" value="maandag"/>
    <meta type="text" name="kleur" value="geel"/>
    <meta type="text" name="kleur" value="groen"/>
  </metadata>
  <node begin="0" cat="top" end="6" id="0" rel="top">
    ...
  </node>
  <sentence sentid="line.3">Dit is de derde zin .</sentence>
</alpino_ds>
```

### <a name="brackets"></a> Instructies voor de parser

Sommige **tokens** hebben een speciale betekenis voor de parser. Wanneer
de tekens deel zijn van een langer token, dan hebben ze geen speciale
betekenis. Zie:
[Alpino User Guide: Special symbols in the input: Bracketed input](http://www.let.rug.nl/vannoord/alp/Alpino/AlpinoUserGuide.html#_bracketed_input)

token | interpretatie door de parser
------|-----------------------------
`[`   | speciaal
`]`   | speciaal
`\[`  | `[`
`\]`  | `]`
`\\[` | `\[`
`\\]` | `\]`

**1—** Wanneer je de tekst laat parsen dan worden tokens zo nodig
automatisch vervangen. (De tokenizer splitst de tekst `\[` in twee
tokens, waarna alleen het tweede token wordt vervangen.)

invoer | tokens
-------|-------
`[`    | `\[`
`]`    | `\]`
`\[`   | `\` `\[`
`\]`   | `\` `\]`
`\\[`  | `\\` `\[`
`\\]`  | `\\` `\]`

**2—** Wanneer je tekst laat parsen die al getokeniseerd is dan kun je
met een escape-level aangeven hoe verschillende tokens geïnterpreteerd
moeten worden. Bijvoorbeeld:

```json
    "data_type": "lines tokens none"
```

Default is `half`.

In onderstaande tabel staat hoe bepaalde tokens (eerste kolom) worden
geïnterpreteerd voor verschillende escape-levels.

token  | `none`   | `half` | `full`
-------|----------|--------|-------
`[`    | speciaal | `[`    | `[`
`]`    | speciaal | `]`    | `]`
`\[`   | `[`      | `[`    | `\[`
`\]`   | `]`      | `]`    | `\]`
`\\[`  | `\[`     | `\[`   | `\[`
`\\]`  | `\]`     | `\]`   | `\]`

## <a name="partok"></a> Tokeniseren van doorlopende tekst

Tokenisatie van doorlopende tekst gebeurt door het programma
`$ALPINO_HOME/Tokenization/partok` met standaardwaardes voor opties
`-i` en `-t`, en de [waarde *prefix*](#user-content-type-text) voor de
optie `-d`.

Tekst wordt opgedeeld in paragrafen en zinnen. Paragrafen worden
genummerd en zinnen worden per paragraaf genummerd.

Lijsten worden herkend doordat regels die voldoen aan deze reguliere
expressie worden gezien als het begin van een nieuwe zin:
`^\s+(\d+[.)]|\*|-)(\s|$)`

Voorbeeld invoer:

```
% Dit is een commentaar.

Dit is een test. Met commentaren
en labels.

Demo van:
 1. paragrafen
 2. labels
 3. commentaren

% Dit is geen label omdat er een | binnenin zit:
Dit is | geen label|

 % Dit is geen commentaar!

% Dit is metadata:
##META text warnings = none

knmi.meta|
Vandaag & morgen

Zonnig zomerweer

% Dit is ook een commentaar.

knmi.main |
##META int mintemp = 14
##META int maxtemp = 26
Het is vandaag een mooie zomerdag. Het is zonnig en droog en de maxima
lopen uiteen van 22°C op de Wadden tot lokaal 26°C in het zuiden van het
land. Vanavond verschijnen er in Limburg enkele wolkenvelden, de kans op
neerslag blijft klein. De noordoostelijke wind is zwak tot matig.
% Een commentaar begint ook een nieuwe paragraaf!
Komende nacht is het helder, maar in het noordoosten kunnen er opnieuw
enkele mistbanken ontstaan. De minimumtemperatuur loopt uiteen van 10
graden in het noordoosten tot 14 graden in het zuiden. Er staat een
zwakke wind uit oost tot noordoost.
% Metadata begint ook een nieuwe paragraaf!
##META int mintemp =
##META int maxtemp = 28
Morgen overdag is het aanvankelijk zonnig en droog. In de loop van de
middag komt er van het zuiden uit meer bewolking opzetten en neemt
vooral in het zuiden de kans op een bui toe. In de avond trekt een
gebied met wat buiige regen van het zuidwesten uit het land binnen. De
maximumtemperatuur ligt tussen 24 graden in het noorden tot lokaal 28
graden in het zuidoosten van het land. De zwakke tot matige wind is
eerst oost maar draait in de loop van de middag naar zuid en wordt dan
in het zuidwesten geleidelijk veranderlijk. (bron: KNMI)

% Wanneer een label herhaalt wordt tellen paragrafen door van
% de laatste keer dat het label werd gebruikt.
knmi.meta|
Toon minder van het weerbericht

% Een leeg label:

    |

Dat was het!

% Einde bestand.
```

Uitvoer:

```
% Dit is een commentaar.
doc.p.1.s.1|Dit is een test .
doc.p.1.s.2|Met commentaren en labels .
doc.p.2.s.1|Demo van :
doc.p.2.s.2|1. paragrafen
doc.p.2.s.3|2. labels
doc.p.2.s.4|3. commentaren
% Dit is geen label omdat er een | binnenin zit:
doc.p.3.s.1|Dit is | geen label|
doc.p.4.s.1|% Dit is geen commentaar !
% Dit is metadata:
##META text warnings = none
knmi.meta.p.1.s.1|Vandaag & morgen
knmi.meta.p.2.s.1|Zonnig zomerweer
% Dit is ook een commentaar.
##META int mintemp = 14
##META int maxtemp = 26
knmi.main.p.1.s.1|Het is vandaag een mooie zomerdag .
knmi.main.p.1.s.2|Het is zonnig en droog en de maxima lopen uiteen van 22°C op de Wadden tot lokaal 26°C in het zuiden van het land .
knmi.main.p.1.s.3|Vanavond verschijnen er in Limburg enkele wolkenvelden , de kans op neerslag blijft klein .
knmi.main.p.1.s.4|De noordoostelijke wind is zwak tot matig .
% Een commentaar begint ook een nieuwe paragraaf!
knmi.main.p.2.s.1|Komende nacht is het helder , maar in het noordoosten kunnen er opnieuw enkele mistbanken ontstaan .
knmi.main.p.2.s.2|De minimumtemperatuur loopt uiteen van 10 graden in het noordoosten tot 14 graden in het zuiden .
knmi.main.p.2.s.3|Er staat een zwakke wind uit oost tot noordoost .
% Metadata begint ook een nieuwe paragraaf!
##META int mintemp =
##META int maxtemp = 28
knmi.main.p.3.s.1|Morgen overdag is het aanvankelijk zonnig en droog .
knmi.main.p.3.s.2|In de loop van de middag komt er van het zuiden uit meer bewolking opzetten en neemt vooral in het zuiden de kans op een bui toe .
knmi.main.p.3.s.3|In de avond trekt een gebied met wat buiige regen van het zuidwesten uit het land binnen .
knmi.main.p.3.s.4|De maximumtemperatuur ligt tussen 24 graden in het noorden tot lokaal 28 graden in het zuidoosten van het land .
knmi.main.p.3.s.5|De zwakke tot matige wind is eerst oost maar draait in de loop van de middag naar zuid en wordt dan in het zuidwesten geleidelijk veranderlijk .
knmi.main.p.3.s.6|( bron : KNMI )
% Wanneer een label herhaalt wordt tellen paragrafen door van
% de laatste keer dat het label werd gebruikt.
knmi.meta.p.3.s.1|Toon minder van het weerbericht
% Een leeg label:
doc.p.5.s.1|Dat was het !
% Einde bestand.
```
