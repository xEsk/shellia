# Programa de correcció estructural de Shellia

## Objectiu

Corregir els vuit punts detectats a l’anàlisi estructural de Shellia mitjançant vuit fases petites, ordenades i verificables. El programa prioritza primer la seguretat i el control del procés, estableix després una base de qualitat estable i deixa les refactoritzacions internes per al final.

El resultat ha de ser una aplicació més segura i mantenible sense reescriure-la, sense introduir arquitectures noves i sense alterar el comportament observable fora de les correccions aprovades.

## Resultat esperat

En acabar el programa:

- les substitucions de comandes no podran aprofitar una ordre exterior aparentment segura per evitar la confirmació;
- només el punt d’entrada del binari podrà finalitzar el procés;
- cada push i pull request tindrà verificació automàtica de qualitat;
- el mòdul Go tindrà una identitat pública instal·lable;
- les dependències entre paquets seran explícites i no estaran amagades darrere d’àlies mutables;
- executor i UI no rebran configuració completa ni tindran accés innecessari a credencials;
- els fluxos interactiu, de planificació i d’execució tindran responsabilitats delimitades;
- l’executor no dependrà del paquet de presentació;
- el contracte JSON de l’LLM serà estricte quan el proveïdor ho permeti i compatible amb els models locals quan no;
- totes les comprovacions de proves, `vet`, lint, build i concurrència definides pel programa passaran.

## Estat inicial i prerequisits

L’estructura general del projecte ja és adequada per a una CLI Go: `cmd/shellia` és un punt d’entrada prim i els dominis privats viuen sota `internal`. El projecte també disposa de `runtimeDeps`, proves focalitzades i mecanismes útils de límit de sortida, cancel·lació de grups de processos i validació d’evidències.

La línia base verificada abans de redactar aquest programa és:

- `env GOCACHE=/tmp/go-build go test -count=1 ./...` passa;
- `go vet ./...` passa;
- les proves amb `-race` dels paquets crítics passen;
- `golangci-lint` detecta 26 incidències que encara s’han de classificar i corregir;
- no hi ha un workflow de CI per a push o pull request;
- hi ha modificacions locals de l’usuari a `README.md`, `internal/app/config_test.go`, `internal/config/config.go`, `internal/config/visual_style_test.go` i `site/index.html` que s’han de preservar.

Cada fase partirà de l’estat real del repositori en aquell moment. Si la línia base canvia abans d’implementar-la, el seu pla haurà de registrar la diferència sense atribuir-se ni revertir canvis aliens.

## Principis d’execució

- Una fase resol un problema principal i produeix un canvi funcional independent.
- Les fases s’executen en ordre i no depenen de treball encara no aprovat d’una fase posterior.
- Es prioritzen seguretat i control del procés, després qualitat i identitat del projecte, i finalment estructura interna.
- Es preserven APIs, configuracions, sortides, codis de retorn, traces i comportaments existents, excepte quan un criteri d’acceptació exigeix corregir-los.
- Es reutilitzen `runtimeDeps`, els paquets actuals i els patrons de proves existents.
- No s’afegeixen frameworks, parsers de shell, contenidors de dependències ni paquets arquitectònics genèrics.
- Cada implementació aplica el canvi més petit que resol completament la fase.
- Cada fase tindrà el seu propi pla d’implementació i commits focalitzats.

## Ordre i dependències

Les fases s’executaran estrictament en aquest ordre:

`1 → 2 → 3 → 4 → 5 → 6 → 7 → 8`

Les fases 1 i 2 eliminen riscos funcionals immediats. La fase 3 crea la barrera automàtica que protegirà la resta de canvis. Les fases 4, 5 i 6 estabilitzen identitat i dependències abans de dividir els grans fluxos. Les fases 7 i 8 fan les descomposicions més sensibles quan la base ja està protegida.

No es començarà una fase si l’anterior no compleix tots els seus criteris d’acceptació i verificació.

## Fase 1: enduriment del classificador de seguretat

### Problema

L’anàlisi d’operadors de shell ignora substitucions `$()` i accents greus quan apareixen dins de cometes dobles. Això permet que una ordre exterior considerada segura amagui una ordre executable. Amb `yes_safe=true`, aquesta classificació pot evitar la confirmació abans d’executar tota l’expressió mitjançant el shell.

Exemple del comportament que s’ha de corregir:

```sh
echo "$(touch /tmp/shellia-demo)"
```

Encara que `echo` sigui una arrel segura, `touch` també s’executa i l’ordre completa no pot ser considerada localment segura.

### Disseny

El classificador actual s’ampliarà perquè reconegui `$()` i accents greus en els contextos on el shell els executa, incloses les cometes dobles. Les cometes simples continuaran tractant el contingut com a literal.

La classificació tindrà en compte l’ordre executable amagada:

- si la substitució conté una arrel perillosa reconeguda, l’ordre completa hereta una classificació d’alt risc;
- qualsevol altra substitució executable deixa de ser `LocalSafe` i exigeix confirmació;
- literals com `echo "a > b"` no es converteixen en falsos positius;
- els operadors escrits dins de cometes simples conserven el comportament literal.

La solució serà una extensió focalitzada de l’escàner existent. No s’incorporarà un parser complet de shell ni una dependència externa.

### Criteris d’acceptació

- `echo "$(touch /tmp/shellia-demo)"` no és `LocalSafe`.
- Una substitució que amagui una arrel destructiva, com `rm`, és d’alt risc.
- Les variants amb accents greus reben el mateix tractament.
- Les substitucions executables fora i dins de cometes dobles requereixen confirmació com a mínim.
- El contingut equivalent dins de cometes simples es manté literal.
- Els casos segurs existents no pateixen regressions.
- Les proves són tabulars i inclouen casos adversaris, niats i de cometes.

## Fase 2: propietat dels errors i del procés

### Problema

`runInteractive` pot cridar un helper de la UI que escriu a l’`stderr` global i executa `os.Exit`. Aquesta sortida profunda evita que `runApp` retorni el codi corresponent i pot saltar-se `defer` importants, com el tancament de traces o l’escriptura de `session_end`.

Exemple: un error de lectura diferent d’EOF durant una sessió interactiva pot matar el procés des del paquet de UI en lloc de propagar-se fins al punt d’entrada.

### Disseny

La propietat del procés quedarà limitada al punt d’entrada de producció de `cmd/shellia/main.go`:

- les funcions internes retornaran errors o resultats explícits;
- `runInteractive` propagarà els errors de lectura i no finalitzarà el procés;
- `runApp` decidirà la sortida d’error i el codi de retorn utilitzant `runtimeDeps`, inclòs `deps.Stderr`;
- els `defer` de sessió i traces s’executaran en tots els camins;
- la UI només formatarà o escriurà presentació, sense tenir autoritat per acabar el procés.

No es crearà un sistema nou d’errors. Es mantindrà l’arquitectura actual basada en valors de retorn i dependències injectades.

### Criteris d’acceptació

- Cap camí intern de producció fora del punt d’entrada crida `os.Exit`.
- Un error de lectura no EOF injectat fa que `runApp` retorni codi `1` sense matar el procés de proves.
- La sortida d’error s’escriu al `Stderr` injectat.
- EOF i cancel·lació conserven el comportament esperat.
- Les traces es tanquen i els esdeveniments finals s’emeten també en camins d’error.

## Fase 3: línia base de CI i qualitat

### Problema

El repositori automatitza releases i GitHub Pages, però no valida cada push o pull request. A més, el lint local detecta incidències de format, wrapping d’errors, assercions, paraules duplicades, bucles i espais que les proves i `vet` no detecten.

Exemple: una construcció d’error amb `%w: %v` només preserva una de les dues causes per a `errors.Is` o `errors.As`, malgrat semblar un wrapping complet.

### Disseny

Primer es classificarà la línia base actual del lint. Les incidències reals es corregiran amb canvis focalitzats i el format s’aplicarà només als fitxers que pertanyin a la fase o amb coordinació explícita si coincideixen amb modificacions de l’usuari.

S’afegirà un workflow de GitHub Actions per a push i pull request que executi:

- compilació del binari;
- suite completa de proves;
- `go vet`;
- `golangci-lint` amb configuració reproduïble;
- proves amb detector de carreres als paquets crítics definits pel projecte.

Els workflows de release i Pages es conservaran. Aquesta fase no canviarà la publicació de versions.

### Criteris d’acceptació

- El lint local passa sense ignorar indiscriminadament incidències existents.
- El workflow s’activa en push i pull request.
- Build, tests, `vet`, lint i comprovació de concurrència són passos visibles i reproduïbles.
- Una fallada en qualsevol comprovació fa fallar la CI.
- Els workflows de release i Pages continuen sent funcionals i separats.
- No es reformategen ni se sobreescriuen canvis locals aliens a la fase.

## Fase 4: identitat canònica del mòdul Go

### Problema

El `go.mod` declara `module shellia`, mentre que el repositori públic és `github.com/xEsk/shellia`. Aquesta identitat local impedeix l’ús estàndard de la ruta remota, per exemple amb `go install github.com/xEsk/shellia/cmd/shellia@latest`.

### Disseny

El mòdul passarà a declarar la ruta canònica `github.com/xEsk/shellia` i totes les importacions internes s’actualitzaran de manera mecànica. També es revisaran `.goreleaser.yaml`, la injecció de versió i la documentació d’instal·lació afectada.

La fase no crearà cap tag, release ni publicació remota.

### Criteris d’acceptació

- `go.mod` declara `github.com/xEsk/shellia`.
- `go list ./...` resol tots els paquets amb la ruta canònica.
- No queden importacions internes amb l’antic prefix local.
- La suite completa i la compilació del binari passen.
- La configuració de release continua apuntant al binari i paquet correctes.
- La documentació mostra la instrucció d’instal·lació remota aplicable quan existeixi una versió publicada.

## Fase 5: eliminació d’àlies i dependències ocultes

### Problema

Alguns paquets reexporten tipus o guarden funcions d’altres paquets en variables mutables. Això amaga el propietari real de la funcionalitat, dificulta el seguiment de dependències i permet substitucions globals accidentals. També hi ha un àlies obsolet a UI que manté una dependència `ui → llm` innecessària.

Exemple: una crida aparentment pròpia d’`app` pot ser en realitat una variable que apunta a `llm.ParseResponse`, de manera que el lector no veu la dependència al punt d’ús.

### Disseny

La neteja es farà en tres passos petits dins de la mateixa fase:

1. Eliminar àlies morts i la dependència obsoleta `ui → llm`.
2. Substituir variables de funció per crides qualificades al paquet propietari i eliminar façanes públiques o privades duplicades.
3. Classificar els àlies de tipus restants i conservar temporalment només els que siguin necessaris per evitar una ruptura injustificada d’API.

L’eliminació de `executor → ui` queda reservada a la fase 7, perquè requereix definir el límit d’orquestració. No es crearà una interfície genèrica únicament per embellir el graf de dependències.

### Criteris d’acceptació

- UI no importa `internal/llm` ni conserva `llmResponse`.
- App crida directament els propietaris reals, com `llm.ParseResponse`.
- No queden variables de funció mutables usades només com a façana.
- Cada àlies de tipus restant té una necessitat de compatibilitat identificada.
- Les proves existents utilitzen injecció explícita o `runtimeDeps`, no mutació global nova.
- No canvia el comportament observable de la CLI.

## Fase 6: vistes estretes de configuració

### Problema

La configuració completa, que conté l’API key, circula per executor i presentació encara que aquests components només necessitin una petita part dels seus camps. Això amplia l’accés a secrets i acobla els paquets a opcions que no els corresponen.

Exemple: una funció d’execució de comandes pot rebre `config.Config` sencera per consultar opcions de seguretat o límits, tot i que també queda capacitada per llegir credencials de l’LLM.

### Disseny

La configuració externa es mantindrà compatible: no canviaran `config.toml`, flags, precedència, selecció de model ni canvi de tema. A la frontera de l’aplicació es derivaran vistes o opcions mínimes per responsabilitat:

- configuració de transport i model per a LLM;
- opcions d’execució i seguretat per a executor;
- opcions de presentació per a UI;
- opcions de memòria i context per a sessió i construcció del prompt.

Es preferiran estructures petites ja existents o noves estructures concretes amb camps necessaris. No s’introduirà un contenidor de dependències ni una jerarquia genèrica de configuració.

### Criteris d’acceptació

- L’API key només és accessible durant càrrega de configuració, selecció de model i transport LLM.
- `executor.ExecuteCommands` i els seus helpers no reben `config.Config` completa.
- UI no té accés a credencials.
- `executor.RuntimeDeps` no conté dependències HTTP que executor no utilitza.
- Els fitxers de configuració existents produeixen el mateix comportament.
- Flags, precedència, model i canvi de tema mantenen les proves de caracterització.

## Fase 7: descomposició de l’orquestració

### Problema

`runInteractive`, `runTurn`, `executeCommands` i altres helpers concentren classificació, planificació, execució, UI, reintents, historial, sessió i evidències. La complexitat fa fàcil que un camí oblidi actualitzar una part de l’estat. Alhora, executor importa UI, cosa que barreja execució mecànica i presentació.

Exemple: dos camins de replanificació poden actualitzar de manera lleugerament diferent els intents, l’evidència o la memòria de sessió, encara que representin el mateix resultat de torn.

### Disseny

`runInteractive` es dividirà segons les responsabilitats que ja conté:

- classificació de l’entrada;
- encaminament de slash commands;
- execució d’un torn;
- aplicació del resultat del torn;
- execució manual de shell.

Una sola funció serà propietària d’aplicar el resultat a historial, reintents, evidències i sessió. `runTurn` delegarà la construcció de la petició de planificació, validació i reparació, decisions `complete` o `blocked`, execució, evidències i límits de planificació.

Per eliminar `executor → ui`:

- executor conservarà l’autoritat sobre classificació de seguretat, confirmació i revalidació després d’una edició;
- el paquet executor definirà el contracte mínim de presentació o confirmació que consumeix;
- app connectarà aquest contracte amb el renderer actual;
- l’execució mecànica no importarà UI.

La fase reorganitzarà principalment funcions i fitxers dins dels propietaris actuals. No crearà un paquet arquitectònic nou ni un motor de workflows alternatiu.

### Criteris d’acceptació

- Hi ha una única funció propietària d’aplicar el resultat d’un torn.
- Cap camí de finalització, bloqueig, reparació o reintent oblida historial, evidències o sessió.
- Executor no importa UI.
- La classificació de seguretat local i la revalidació després d’editar continuen sent obligatòries.
- Els transcripts visuals, codis de sortida i esdeveniments de trace es conserven.
- Les proves de caracterització del bucle, planificació, límits i cancel·lació continuen passant.

## Fase 8: contracte LLM i descomposició del prompt

### Problema

El prompt exigeix exactament un objecte JSON, però el parser accepta text anterior o posterior i pot ignorar un segon objecte. Al mateix temps, `buildUserPrompt` combina autoritat, context, memòria, evidències, intents, errors de reparació i pressupostos en una funció molt complexa.

Exemple: una resposta com `text previ {"action":"complete",...} text posterior` pot ser acceptada malgrat incomplir el contracte declarat.

### Disseny

El parser tindrà dos modes explícits:

- mode estricte quan el proveïdor admet `response_format`: la resposta completa ha de ser un únic document JSON amb un únic objecte, sense prefix, sufix ni segon objecte;
- mode compatible quan el proveïdor no admet `response_format`: es preserva l’extracció tolerant del primer objecte per mantenir compatibilitat amb models locals.

El mode estricte validarà el document únic i el contracte funcional actual. No rebutjarà camps desconeguts només per una preferència d’estil si el contracte vigent no ho exigeix.

La divisió de `buildUserPrompt` seguirà les seccions conceptuals existents:

- autoritat i objectiu;
- context i historial;
- memòria de sessió;
- evidències i intents;
- decisió anterior i errors de reparació;
- pressupostos, truncaments i omissions.

La primera extracció serà mecànica i haurà de preservar exactament el text generat. Qualsevol canvi posterior del contingut o estratègia del prompt requerirà una decisió separada.

### Criteris d’acceptació

- El mode estricte rebutja prefixos, sufixos, documents concatenats i múltiples objectes.
- El mode compatible conserva els casos tolerants necessaris per a models locals.
- La selecció del mode deriva de la capacitat real de `response_format`.
- La validació d’acció, objectiu, finalització i camps obligatoris no canvia accidentalment.
- El comportament de risc continua sent determinat localment i no per l’LLM.
- Les proves golden o equivalents demostren que la divisió de `buildUserPrompt` no canvia la sortida.
- La funció principal del prompt deixa de barrejar selecció d’evidències, càlcul de pressupostos i format de totes les seccions.

## Correspondència amb l’anàlisi

El programa cobreix tots els punts originals una sola vegada com a responsabilitat principal:

1. Substitucions de shell no detectades: fase 1.
2. `os.Exit` profund i `defer` omesos: fase 2.
3. Absència de CI i de línia base de lint: fase 3.
4. Ruta de mòdul Go no canònica: fase 4.
5. Àlies mutables i dependències ocultes: fase 5.
6. Propagació de configuració completa i secrets: fase 6.
7. Funcions monolítiques i dependència `executor → ui`: fase 7.
8. Contracte JSON permissiu i prompt monolític: fase 8.

Les fases poden tocar punts relacionats com a preparació, però no absorbiran l’abast principal d’una altra fase.

## Verificació de cada fase

Cada fase seguirà aquesta seqüència mínima:

1. Afegir o identificar una prova focalitzada que representi el criteri d’acceptació.
2. Quan sigui una correcció, demostrar que la prova detecta el comportament anterior incorrecte.
3. Aplicar la implementació mínima.
4. Executar les proves focalitzades del paquet afectat.
5. Executar la suite completa, `go vet`, lint i build.
6. Executar `-race` quan la fase afecti bucles, traces, execució, UI compartida o estat concurrent.
7. Revisar que el diff no inclou canvis aliens ni sobreescriu modificacions de l’usuari.
8. Registrar el resultat i els riscos residuals abans d’avançar.

Els plans d’implementació concretaran les ordres exactes i els fitxers previstos per a cada fase segons l’estat del repositori en aquell moment.

## Prova final del programa

El programa només es considerarà complet quan es pugui demostrar conjuntament que:

- els exemples adversaris de substitució de shell no són segurs sense confirmació;
- cap error intern finalitza directament el procés;
- la CI obligatòria cobreix build, tests, `vet`, lint i concurrència;
- la identitat del mòdul i les importacions són canòniques;
- no queden façanes mutables ni dependències obsoletes identificades;
- les credencials no arriben a executor o UI;
- cada resultat de torn té un únic camí d’aplicació coherent;
- executor no depèn de UI;
- el mode JSON estricte i el compatible compleixen els seus contractes diferenciats;
- la suite completa passa des d’un arbre de treball controlat;
- el binari es compila i els fluxos interactiu i one-shot conserven el comportament acordat.

## Portes d’aturada

Una fase s’aturarà i requerirà una decisió explícita si apareix:

- un canvi incompatible d’API o configuració que no estigui previst;
- una ampliació significativa de l’abast;
- un comportament existent que no es pugui determinar si és intencionat;
- una prova general, de lint o de concurrència que falla per la fase;
- un conflicte amb modificacions locals de l’usuari;
- la necessitat d’afegir una dependència, arquitectura o abstracció no aprovada;
- una correcció que depengui d’implementar anticipadament una fase posterior.

No s’avançarà a la fase següent fins a resoldre la porta d’aturada o ajustar formalment aquest disseny.

## Fora d’abast

- Funcionalitats noves per a l’usuari.
- Redissenys visuals o canvis dels temes existents.
- Canvis de proveïdor, model o estratègia general de l’LLM.
- Optimitzacions de rendiment no motivades per una regressió mesurada.
- Reescriptura del bucle interactiu o del motor d’execució.
- Canvi de format de `config.toml` o de precedència entre configuració i flags.
- Nous frameworks, parsers de shell, contenidors de dependències o sistemes de plugins.
- Creació de tags, releases, publicacions o canvis al web públic.
- Refactoritzacions no relacionades amb un criteri d’acceptació d’aquest programa.
