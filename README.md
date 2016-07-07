# Alpino API

Een API voor een Alpino-server.

Deze API wordt onder andere gebruikt door [PaQu](https://github.com/rug-compling/paqu).

## Status

Deze API is nog in ontwikkeling. Meedenkers zijn welkom.

### TO DO

 * Dit vertalen in het Engels? Beoogde gebruikers begrijpen Nederlands.

## Eigenschappen van de server

Afhankelijk van de manier waarop Alpino wordt aangeroepen hebben rechte
haken in de tekst wel of geen speciale betekenis. Voor deze API geldt
dat rechte haken **geen** speciale betekenis hebben.

Alternatief voorstel:

 * Met een parse-request kan de optie `"hint":true` gebruikt worden om
   aan te geven dat rechte haken wel een speciale betekenis hebben.
   Als de server deze mogelijkheid niet ondersteunt is de reactie een
   `501 Not Implemented`.
 * In de response van een info-request is er een `"has_hint":true` of
   `"has_hints":false` om aan te geven of de server deze feature
   ondersteunt.

## Request en reply

Deze API beschrijft hoe je met json via http kunt communiceren met een
server die Alpino gebruikt om tekst te parsen. 

Elke verzoek aan de server bestaat uit een json-object. Daarna kan nog
data volgen als platte tekst.

Elk verzoek bevat een element `request` die aangeeft wat de opdracht aan
de server is. 

request | omschrijving
--------| ------------
parse   | verzoek om tekst te parsen
output  | verzoek om (een deel van) de resultaten van een parse terug te sturen
cancel  | verzoek om een parse te annuleren
info    | verzoek om informatie over de server

Elk resultaat verstuurd door de server is een json-object,
met tenminste de elementen `code` en `status`.

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
`json` terug stuurt, maar alleen een http-statuscode in de headers.

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

## Lijst van requests

### 
