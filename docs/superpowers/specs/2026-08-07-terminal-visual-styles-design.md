# Disseny d’estils visuals configurables per a la conversa de terminal

## 1. Resum de la decisió

Shellia incorporarà quatre estils visuals estructurals seleccionables des de la configuració persistent:

- `plain`: interfície actual, compacta i neta;
- `guide`: guia vertical que diferencia els torns de l’usuari i de Shellia;
- `bands`: bandes subtils per torn;
- `cards`: targetes compactes amb renderitzat incremental.

Els estils no permetran configurar colors. Reutilitzaran la paleta actual i canviaran només la geometria, la jerarquia i la separació dels blocs.

`plain` serà el valor per defecte i conservarà la sortida actual. `--no-color` eliminarà únicament els estils ANSI generats per Shellia, però mantindrà la geometria de l’estil seleccionat. Quan la sortida principal no sigui un TTY o `TERM=dumb`, l’estil efectiu serà `plain` i no s’emetrà ANSI.

La feature continuarà sent scrollback-native. No introduirà una TUI de pantalla completa, alternate screen, bucle d’esdeveniments nou ni dependències noves.

Cada estil serà un mòdul compilat, separat i substituïble darrere d’un contracte intern comú. Aquesta modularitat serà “plugin-like”, però no s’introduirà càrrega dinàmica, discovery, reflection, registre extern ni una API pública de plugins.

## 2. Problema i valor per a l’usuari

La UI actual és agradable durant l’execució, però una conversa llarga és difícil de revisar perquè comparteixen massa pes, alineació i tractament:

- les peticions de l’usuari;
- els plans i explicacions de Shellia;
- els comandos, confirmacions i outputs del sistema;
- la resposta final.

Els separadors horitzontals marquen fases, però no comuniquen prou bé qui parla ni quina activitat depèn de quin torn. La conseqüència és que el flux s’entén mentre passa, però perd jerarquia quan es rellegeix.

El valor de la feature és permetre que cada persona triï el nivell de separació que prefereix sense alterar el comportament, la seguretat o els colors de Shellia.

## 3. Resultat observable i criteris d’èxit

La feature es considerarà correcta quan:

1. una configuració existent sense `ui.style` produeixi la mateixa sortida `plain` que avui;
2. els quatre valors es puguin seleccionar des de `config.toml`;
3. l’estil s’apliqui igualment als modes interactiu i one-shot quan stdout sigui un TTY;
4. plans, execucions, outputs i respostes finals quedin subordinats al torn de Shellia en els tres estils nous;
5. `--no-color` conservi rails, vores, indentació i espaiat sense emetre ANSI propi de Shellia;
6. pipes, redireccions i `TERM=dumb` degradin automàticament a `plain`;
7. els outputs no interactius continuïn apareixent en temps real;
8. els comandos interactius continuïn controlant el terminal sense prefixes, vores ni transformacions del seu output;
9. cap estil canviï plans, riscos, confirmacions, execució, evidència, traces o memòria de sessió;
10. cada estil tingui implementació i proves pròpies, sense condicionals sobre altres estils;
11. pel que fa al renderer, afegir un estil compilat nou requereixi una implementació nova i una única entrada al selector, no editar els renderers existents.

## 4. Direcció visual

### 4.1 Domini

La jerarquia surt del domini propi de Shellia: torns, intenció, pla, pas, comando, confirmació, output, evidència i resposta final. La conversa té dos actors principals —usuari i Shellia— i l’activitat tècnica és una capa subordinada a Shellia, no un tercer interlocutor equivalent.

### 4.2 Paleta fixa

Es manté el món cromàtic existent:

- grafit per al canvas i superfícies;
- cian per a l’usuari;
- magenta per a Shellia i els plans;
- blau per als comandos;
- groc per a confirmacions o atenció;
- gris per a metadades i output del sistema.

No s’afegeixen claus de configuració per a colors, intensitat, vores o espaiat.

### 4.3 Element distintiu

La signatura compartida dels estils nous és la superfície de torn: una identitat visual estable per a l’usuari i una altra per a Shellia, amb plans i execucions niats dins del torn de Shellia.

### 4.4 Defaults rebutjats

- Separar només amb color: no resol la lectura amb `--no-color` ni en scrollback llarg.
- Envoltar cada línia amb una caixa independent: fragmenta massa la conversa.
- Convertir Shellia en una aplicació full-screen: trenca el model CLI-first, el scrollback i la composició amb altres eines Unix.
- Centralitzar els quatre estils en un renderer gegant ple de branques: acobla variants que han de poder evolucionar independentment.
- Crear un sistema de plugins dinàmics: afegeix infraestructura, compatibilitat i superfície pública que aquesta feature no necessita.

### 4.5 Referència visual canònica

La composició aprovada no depèn només d’aquesta descripció. El repositori inclou la [referència visual interactiva dels quatre estils](assets/terminal-visual-styles-reference.html), construïda amb el transcript real que va originar el disseny.

L’HTML és un contracte de disseny versionat, no codi de producció ni una simulació pixel-perfect del terminal. Durant la implementació serà normatiu per a:

- la jerarquia entre usuari, Shellia i activitat tècnica;
- el niament de plans, passos, comandos, confirmacions i output;
- la identitat i propietat de cada superfície;
- la indentació relativa, la densitat i els espais entre fases;
- la presència de guies, bandes, vores i separadors segons l’estil;
- el mapa semàntic dels colors existents.

S’adaptaran al terminal, sense alterar aquesta jerarquia:

- el wrapping segons l’amplada real;
- els glifs exactes de vores i separadors;
- la reproducció concreta dels colors i fons per part de cada emulador;
- l’absència de radi, ombres i chrome propis del navegador;
- la suspensió de superfícies durant un handoff PTY interactiu.

Si la implementació necessita apartar-se d’un element normatiu, cal actualitzar i reaprovar primer aquesta especificació i la referència visual. La maqueta temporal de la sessió de disseny no és autoritativa; ho és aquest fitxer versionat.

## 5. Contracte de configuració

La configuració persistent afegirà una única clau:

```toml
[ui]
style = "plain"
```

Valors acceptats:

```text
plain | guide | bands | cards
```

El tipus runtime viurà a `internal/config`, al mateix límit que `NoColor`, `Verbose`, `ShowSystemOutput` i `ShowCommandPopup`. No es mourà a `internal/core` perquè és una decisió exclusiva de presentació.

El valor es normalitzarà amb `strings.TrimSpace` i minúscules, seguint els normalitzadors de configuració existents. Un valor buit o desconegut conservarà el fallback actual, que per defecte serà `plain`. No s’introduirà un nou règim d’errors només per a aquesta clau.

La plantilla generada documentarà `style = "plain"` i els quatre valors. Aquesta feature no afegirà un flag CLI, variable d’entorn ni slash command per canviar l’estil.

## 6. Estil configurat i presentació efectiva

El codi deixarà de tractar un únic booleà `ui` com si representés alhora color, capacitat del terminal i geometria.

La decisió efectiva tindrà dos eixos:

- estil estructural efectiu;
- ANSI habilitat o deshabilitat.

La matriu serà:

| Destinació | Configuració | Estil efectiu | ANSI de Shellia |
| --- | --- | --- | --- |
| TTY normal | `style=plain` | `plain` | sí |
| TTY normal | `style=guide/bands/cards` | configurat | sí |
| TTY amb `--no-color` | qualsevol estil | configurat | no |
| stdout no-TTY | qualsevol estil | `plain` | no |
| `TERM=dumb` | qualsevol estil | `plain` | no |

La capacitat es resoldrà una vegada per procés a partir de l’stdout real de `runtimeDeps`, que és l’autoritat principal també quan stderr tingui una destinació diferent. `runtimeDeps` exposarà una dependència estreta de detecció de terminal que, en producció, delegarà a `term.IsTerminal` amb `deps.Stdout.Fd()`. No es consultarà directament `os.Stdout` fora del process entry point. Els tests substituiran aquesta dependència, no els globals del procés.

`--no-color` només afecta les seqüències generades per Shellia. No es filtraran ni reescriuran seqüències ANSI que provingui d’un procés fill, preservant el comportament actual.

## 7. Comportament dels quatre estils

### 7.1 `plain`

Manté els renderers, espais, separadors, prefixes i ordre actuals. És el fallback per compatibilitat i per a sortides no interactives.

La feature haurà de demostrar amb proves de regressió que la selecció implícita o explícita de `plain` no introdueix canvis visuals no relacionats.

### 7.2 `guide`

- Mentre espera entrada, el prompt interactiu identifica l’usuari actiu (`xesc ›`); `›` és un indicador d’edició i no forma part del transcript consolidat.
- En enviar-se, el torn de l’usuari rep una guia vertical cian, una sola etiqueta amb el nom de l’usuari actiu i el text en una fila separada sense `you` ni `›`.
- Totes les files del torn d’usuari comparteixen una superfície compacta de fons blau-petroli molt fosc, sense vores, que acaba poc després del contingut més ample en lloc d’omplir el terminal.
- La superfície aporta una columna de padding intern a cada costat. La seva amplada és la de la fila visible més llarga més aquest padding, limitada per l’amplada disponible del terminal, i s’aplica a totes les files d’un prompt multilínia.
- El torn de Shellia rep una guia vertical magenta.
- Plans, passos i resposta final comparteixen aquesta identitat de torn.
- Cada execució conserva el command box actual i usa una guia secundària neutra per a propòsit, confirmació i output.
- Els separadors globals es redueixen a límits entre torns complets.

Sense color, la superfície de l’usuari conserva el text, el padding i l’ajust de línies, però omet el fons ANSI; les guies i la indentació continuen diferenciant els actors.

### 7.3 `bands`

- Cada torn usa un marcador lateral més ample i una superfície de fons ANSI subtil.
- L’usuari i Shellia mantenen el mateix mapa semàntic de colors que `guide`.
- Les execucions usen una superfície neutra subordinada, sense competir amb el torn.
- Sense color, la banda es degrada a marcador lateral, espaiat i indentació; no es converteix en `plain`.

### 7.4 `cards`

- El torn de l’usuari i el de Shellia es presenten com targetes compactes.
- Plans i execucions poden usar superfícies internes, però no creen una targeta independent per cada línia.
- Les vores es dibuixen amb caràcters Unicode ja coherents amb la UI actual.
- La targeta de Shellia és incremental: s’obre abans del primer contingut, emet laterals mentre arriben plans i outputs, i es tanca quan el torn arriba al resultat terminal.
- El command output no es bufferitza fins al final només per aconseguir una caixa tancada.

Sense color, les vores i el padding continuen definint la targeta.

## 8. Streaming i comandos interactius

`prefixedWriter`, `stepBox.OutputLabel` i `stepBox.OutputLine` ja proporcionen un límit semàntic per processar stdout i stderr no interactius línia per línia. Els estils nous reutilitzaran aquest flux; no classificaran ni interpretaran novament l’output dins de l’executor.

Per a `cards`, el renderer mantindrà l’estat mínim necessari per saber si la superfície està oberta i assegurar-ne el tancament en èxit, error, timeout, cancel·lació o rebuig. L’escriptura continuarà sent incremental. Les línies parcials es tancaran mitjançant el `Flush` existent.

Els comandos que prenen control del terminal mitjançant PTY són un límit explícit:

1. Shellia tanca o suspèn la superfície visual abans del handoff;
2. el procés interactiu rep el PTY, la mida i els bytes sense transformació;
3. Shellia reprèn el seu estil després que el procés retorni;
4. el contingut del procés interactiu no queda dins de rails, bandes o targetes.

Aquest comportament preserva editors, selectors, prompts interactius, barres de progrés i aplicacions que mouen el cursor.

## 9. Arquitectura i reutilització

La implementació es limitarà als límits existents:

- `internal/config`: tipus de l’estil, default, normalització, merge TOML i plantilla;
- `internal/app`: resolució de la presentació efectiva des de `runtimeDeps.Stdout` i obertura/tancament dels torns;
- `internal/ui`: primitives de superfície, prefixes, wrapping, vores i renderitzat de cada estil;
- `internal/executor`: connexió de l’output no interactiu amb la superfície existent i suspensió per a PTY interactiu;
- `README.md`: documentació de la clau i els valors.

### 9.1 Contracte intern de renderer

`internal/ui` definirà un contracte intern petit orientat a semàntica de presentació, no a detalls de negoci. El contracte cobrirà les operacions que varien visualment:

- obrir i tancar un torn d’usuari o de Shellia;
- presentar un pla o bloc informatiu;
- obrir, alimentar i tancar una execució;
- escriure labels, línies d’output i resposta final;
- suspendre i reprendre la superfície al voltant d’un handoff PTY.

El contracte rebrà contingut ja decidit pel workflow i metadades visuals acotades. No rebrà autoritat per classificar risc, decidir confirmacions, modificar comandos ni interpretar outputs.

La selecció de la implementació es farà una sola vegada mitjançant una factory o selector exhaustiu. Fora d’aquest punt no hi haurà switches globals per `plain`, `guide`, `bands` o `cards`.

### 9.2 Separació física dels estils

L’organització prevista és:

```text
internal/ui/
  visual_renderer.go
  visual_surface.go
  visual_plain.go
  visual_guide.go
  visual_bands.go
  visual_cards.go
```

Els noms exactes es podran adaptar a les convencions descobertes durant la implementació, però es mantindran aquestes responsabilitats:

- `visual_renderer.go`: contracte intern, selector únic i tipus semàntics mínims;
- `visual_surface.go`: primitives compartides de width, padding, ANSI, wrapping i escriptura incremental;
- un fitxer per estil: només geometria i decisions visuals d’aquell estil.

`plain` encapsularà el comportament actual en lloc de quedar com una acumulació de casos especials. `guide`, `bands` i `cards` no s’importaran ni es cridaran entre ells.

Les proves seguiran la mateixa separació, amb un fitxer focalitzat per estil i proves comunes de contracte reutilitzades com a suite compartida.

### 9.3 Reutilització de primitives existents

Es reutilitzaran:

- `renderPanel` per a plans i blocs informatius;
- `stepBox` per a comandos, confirmacions i output;
- `renderCommandBox` i `visibleWidth` per a amplada i wrapping;
- `prefixedWriter` per al streaming línia per línia;
- `runtimeDeps` per evitar dependències directes de globals en proves.

La separació modular no duplicarà contingut ni lògica de negoci. Les mateixes primitives semàntiques —torn, pla, execució, output i resposta— alimentaran qualsevol renderer. Cada implementació serà independent en geometria, però compartirà les utilitats mecàniques i el contracte.

No es farà un refactor general de tot `internal/ui`. Només es mouran o extrauran les peces necessàries per establir el contracte i encapsular els paths afectats per aquesta feature.

## 10. Flux de dades

1. Es carrega `ui.style` amb la resta de la configuració.
2. El default explícit és `plain`.
3. `runApp` determina si l’stdout real és un TTY utilitzable.
4. Es deriva l’estil efectiu i la capacitat ANSI segons la matriu definida.
5. El selector crea una única implementació de renderer per a l’estil efectiu.
6. El prompt interactiu reimprimeix el text enviat amb la superfície d’usuari seleccionada.
7. `runTurn` obre una superfície de Shellia abans del primer pla o resposta.
8. Plans, passos, confirmacions, outputs i resposta final passen pel contracte comú dins d’aquesta superfície.
9. El resultat terminal tanca la superfície, inclosos errors i cancel·lacions.
10. El següent prompt inicia un torn nou.

L’estil no entra al prompt del model, l’estat de workflow, la memòria de sessió ni les traces de decisió.

## 11. Errors, degradació i compatibilitat

- Config sense `ui.style`: `plain`.
- Valor buit o desconegut: conserva el valor runtime anterior durant el merge; com que el default inicial és `plain` i no hi ha cap altra font per a aquesta clau, una configuració invàlida acaba efectivament en `plain`.
- No-TTY o `TERM=dumb`: `plain` sense ANSI.
- `--no-color`: estil seleccionat sense ANSI propi de Shellia.
- Amplada estreta: les primitives existents recalculen wrapping deixant espai per al rail o les vores.
- Línia parcial sense newline: es renderitza en `Flush`.
- Error, timeout o cancel·lació amb targeta oberta: es tanca la superfície abans de retornar.
- Procés PTY interactiu: output natiu fora de la superfície.

Els caràcters Unicode es mantenen perquè Shellia ja usa bullets, arrows i separadors Unicode. No s’afegeix una detecció de locale ni una paleta ASCII paral·lela en aquesta feature.

## 12. Seguretat i autoritat

La feature és exclusivament de presentació:

- no canvia la classificació local de risc;
- no redueix ni elimina confirmacions;
- no modifica comandos ni arguments;
- no concedeix autoritat d’execució;
- no transforma output del procés en instruccions;
- no afecta captures, evidència o límits de truncament;
- no envia la preferència visual al model.

Qualsevol implementació que necessiti modificar contractes del planner, workflow, safety o autoritat queda fora d’abast.

## 13. Estratègia de proves

### 13.1 Configuració

- default absent i explícit `plain`;
- càrrega dels quatre valors;
- normalització de majúscules i espais;
- fallback d’un valor desconegut;
- plantilla generada amb la clau documentada;
- configuracions antigues i claus obsoletes continuen acceptades.

### 13.2 Renderers

- snapshots o assertions estructurals per estil;
- fixtures de conversa derivats dels mateixos blocs semàntics de la referència visual canònica;
- `plain` conserva la sortida actual;
- els renderers amb `--no-color` no generen escapes propis de Shellia però conserven geometria;
- wrapping amb rails i vores a diferents amplades;
- resposta Markdown, command boxes, session banner i prompts continuen alineats;
- targetes es tanquen una sola vegada en tots els resultats terminals;
- la suite comuna del contracte passa per als quatre renderers;
- cada renderer es prova des del seu propi fitxer sense dependre d’un altre estil;
- el selector és exhaustiu i concentra l’únic mapping entre valor configurat i implementació.

### 13.3 Streaming

- chunks amb línies completes i parcials;
- UTF-8 dividit entre writes;
- output sense newline final;
- stdout i stderr amb output;
- línies llargues;
- `\r` i seqüències ANSI procedents del fill sense transformació nova;
- errors de writer i camins de `Flush`.

### 13.4 Integració

Matriu mínima:

- quatre estils;
- color i `--no-color`;
- TTY i no-TTY;
- interactiu i one-shot;
- comando no interactiu i handoff PTY interactiu.

Les proves de bucle usaran `runtimeDeps` i streams injectats. No assignaran globals de procés.

## 14. Compatibilitat i desplegament

La feature s’entrega amb `plain` com a default. No hi haurà migració de config ni canvi automàtic d’estil per a instal·lacions existents.

L’ordre recomanat d’implementació és:

1. contracte de configuració i presentació efectiva;
2. regressió de `plain`;
3. contracte intern, selector únic i suite comuna;
4. extracció de `plain` com a implementació aïllada;
5. `guide`;
6. `bands`;
7. `cards` incremental;
8. degradació `--no-color`, no-TTY i PTY;
9. documentació i matriu final de proves.

## 15. Stop Gates d’implementació

Cal aturar i revisar el disseny si:

1. `plain` deixa de conservar la sortida actual sense una decisió explícita;
2. `cards` requereix bufferitzar tot el command output i perdre streaming;
3. un comando PTY deixa de rebre bytes o mida de terminal sense transformació;
4. `--no-color` necessita filtrar output del procés fill;
5. l’estil entra en prompts, workflow, traces de decisió o política de seguretat;
6. cal afegir una dependència de TUI full-screen;
7. la implementació duplica contingut o lògica de negoci en quatre renderers complets;
8. una variant necessita modificar el fitxer d’una altra variant;
9. apareixen condicionals d’estil fora del selector o de la implementació propietària;
10. el contracte visual creix fins a absorbir decisions del workflow, executor o safety.

## 16. Decisió final

La feature és un increment coherent i acotat. Reutilitza els renderers i writers actuals, preserva la naturalesa CLI de Shellia i resol el problema de rellegir converses llargues sense obligar tots els usuaris a adoptar una interfície més carregada.

La recomanació és implementar els quatre estils amb `plain` per defecte, separar geometria d’ANSI, i usar `cards` incremental amb output PTY natiu fora de la superfície.
