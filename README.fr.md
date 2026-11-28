<img src="whatsapp-go/assets/logo.svg" width="96" height="96" alt="askmywa logo">

# askmywa — WhatsApp pour Claude

**[askmywa.com](https://askmywa.com/fr/)** · *[English version](README.md)* · [Politique de confidentialité](https://askmywa.com/fr/confidentialite/)

[![Ruwad-io/askmywa MCP server](https://glama.ai/mcp/servers/Ruwad-io/askmywa/badges/score.svg)](https://glama.ai/mcp/servers/Ruwad-io/askmywa)

Cette extension permet à Claude de lire, chercher et envoyer vos messages WhatsApp, depuis l'application Claude sur votre Mac ou votre PC Windows.

Vous pouvez par exemple lui demander :

- « Résume-moi ce qui s'est dit dans le groupe de l'immeuble cette semaine. »
- « Qu'est-ce que Karim m'a demandé mardi ? »
- « Retrouve le message où on parlait du devis du plombier. »
- « Prépare une réponse à Sophie pour lui dire que je serai en retard. »

Les outils d'envoi renvoient un aperçu par défaut. Claude reçoit la consigne de vous montrer le destinataire et le contenu, puis de demander votre accord avant de confirmer l'envoi. askmywa ne peut pas vérifier indépendamment que l'assistant a obtenu votre accord.

[Voir la démo de 53 secondes](https://askmywa.com/fr/#demo) · [Voir un exemple illustré avec ses messages sources](https://askmywa.com/fr/#exemple). Les messages sont fictifs. La vidéo présente une recherche et un aperçu réellement exécutés en local, dans une interface de présentation, sans connexion ni envoi WhatsApp.

## Ce qu'il vous faut

- Un Mac (Apple Silicon ou Intel) ou un PC sous Windows 10 ou 11.
- L'application [Claude pour Mac ou Windows](https://claude.ai/download), connectée à votre compte.
- WhatsApp sur votre téléphone.

C'est tout. Pas de terminal, pas de code.

## Installer

1. Téléchargez le fichier **[`askmywa.mcpb`](https://github.com/Ruwad-io/askmywa/releases/latest/download/askmywa.mcpb)**, la dernière version, sur [askmywa.com](https://askmywa.com/fr/) ou sur la [page des versions](https://github.com/Ruwad-io/askmywa/releases/latest).
2. Double-cliquez dessus. Claude s'ouvre et vous demande de confirmer l'installation : acceptez. Le même fichier fonctionne sur Mac et sur Windows.
3. Ouvrez une nouvelle conversation dans Claude et écrivez : **« Connecte mon WhatsApp »**. Claude vous demande l'autorisation d'utiliser les outils d'askmywa : choisissez les autorisations qui vous conviennent. Les outils d'envoi renvoient un aperçu par défaut ; l'assistant reçoit la consigne de demander votre accord avant de confirmer un envoi.
4. Claude vous demande votre numéro de téléphone, avec l'indicatif du pays (par exemple 33 6 12 34 56 78 pour la France). Il vous donne ensuite un **code de 8 caractères**.
5. Sur votre téléphone, ouvrez WhatsApp :
   - **iPhone** : Réglages > Appareils connectés > Connecter un appareil.
   - **Android** : menu ⋮ > Appareils connectés > Connecter un appareil.
   
   Choisissez **« Connecter avec un numéro de téléphone »** en bas de l'écran, puis tapez le code. Vous avez environ deux minutes.

Les messages fournis par WhatsApp apparaissent dans les minutes qui suivent. Le lien tient même après un redémarrage de l'ordinateur. WhatsApp peut ne transmettre qu'une partie de votre ancien historique : la recherche porte sur l'archive locale, qui ne contient pas forcément tous les messages du téléphone. Demandez à l'assistant les messages stockés les plus anciens et les plus récents d'une conversation avant de vous fier à un résumé.

## Utiliser

Parlez à Claude normalement. Quelques exemples :

| Vous voulez | Demandez |
|---|---|
| Retrouver un message | « Retrouve le message de Paul sur le loyer » |
| Faire le point | « Quels messages importants ai-je reçus aujourd'hui ? » |
| Lire une conversation | « Montre-moi les derniers messages avec maman » |
| Envoyer un message | « Envoie à Julie : je pars dans 10 minutes » |
| Récupérer une pièce jointe | « Télécharge la photo que Marc m'a envoyée hier » |
| Envoyer un fichier | « Envoie le devis à Ahmed » (voir ci-dessous) |

La recherche ignore les majuscules et les accents : « ecole » trouve « école ».

**Pour envoyer un fichier**, posez-le d'abord dans le dossier **Documents > WhatsApp MCP** (il est créé automatiquement). Claude ne peut envoyer que les fichiers de ce dossier : c'est voulu, pour qu'il ne puisse pas fouiller le reste de votre ordinateur. Vous pouvez choisir un autre dossier dans les réglages de l'extension.

## Vos données

- **L'archive est stockée sur votre ordinateur.** Aucun serveur de ce projet ne la reçoit.
- **Les résultats consultés sont transmis au fournisseur de l'IA.** Ils peuvent inclure le texte des messages, les noms et numéros de téléphone : Anthropic pour Claude, OpenAI pour ChatGPT et Codex. Leurs politiques de confidentialité s'appliquent.
- **Les envois montrent un aperçu par défaut.** La confirmation est fournie par l'assistant, qui reçoit la consigne d'obtenir votre accord. askmywa n'a pas de fenêtre d'approbation indépendante.

## À savoir avant d'installer

- **Ce n'est pas un produit de WhatsApp.** L'extension se connecte comme un appareil lié, de la même façon que WhatsApp Web, mais par une interface que WhatsApp n'a pas officiellement ouverte. Les conditions d'utilisation de WhatsApp ne l'autorisent pas, et WhatsApp pourrait restreindre ou bannir un compte qui l'utilise.
- **Méfiez-vous des messages piégés.** Un message reçu pourrait contenir des instructions destinées à tromper l'assistant. Le parcours d'aperçu et de confirmation repose sur le respect des consignes par l'assistant : lisez l'aperçu avant de dire oui.
- **L'ordinateur doit être allumé** pour que de nouveaux messages arrivent dans l'archive. WhatsApp fournit des messages de rattrapage à la reconnexion, sans garantie d'historique complet.

## Désinstaller

1. Demandez à Claude : **« Déconnecte WhatsApp de cet ordinateur et efface les messages »**. Il vous montre ce qu'il va faire et attend votre accord. Ensuite, l'ordinateur disparaît des appareils connectés de votre téléphone, le service en arrière-plan s'arrête et l'archive des messages est effacée. Les fichiers de votre dossier d'envoi ne sont jamais supprimés.
2. Dans Claude, ouvrez Réglages > Extensions et désinstallez **WhatsApp**.

Faites bien l'étape 1 avant l'étape 2 : sans l'extension, Claude ne peut plus arrêter le service. Si vous avez désinstallé l'extension trop tôt, retirez l'ordinateur depuis votre téléphone (WhatsApp > Appareils connectés), puis réinstallez l'extension le temps de faire l'étape 1.

## Problèmes fréquents

| Ce qui se passe | Quoi faire |
|---|---|
| Le code est refusé ou a expiré | Demandez un nouveau code à Claude (« redonne-moi un code »). Vérifiez que le numéro commence par l'indicatif du pays, sans le 0. |
| Claude dit que WhatsApp n'est pas connecté | Demandez « Quel est l'état de WhatsApp ? » : Claude vous dira quoi faire. |
| Les vieux messages manquent | WhatsApp n'envoie qu'une partie de l'historique au moment de la connexion. Les nouveaux messages, eux, arrivent tous. |
| WhatsApp a déconnecté l'appareil | Redemandez « Connecte mon WhatsApp » et refaites l'étape du code. |
| Une photo ancienne ne se télécharge pas | WhatsApp ne garde les pièces jointes que quelques semaines sur ses serveurs. |

Un autre souci ? [Ouvrez un ticket](https://github.com/Ruwad-io/askmywa/issues) en décrivant ce que vous avez fait et ce qui s'est passé.

## Sans l'extension : Linux, Raspberry Pi et Mac

Le programme fonctionne aussi sous Linux, y compris sur un Raspberry Pi. Un Pi laissé allumé garde l'archive à jour même quand votre ordinateur dort.

Dans un terminal (sur le Pi, en vous y connectant en SSH), une seule commande télécharge la bonne version, vérifie son intégrité, installe le service en arrière-plan et vous propose de lier WhatsApp avec un code à taper sur votre téléphone :

```bash
curl -fsSL https://github.com/Ruwad-io/askmywa/releases/latest/download/install.sh | sh
```

Relancez-la pour mettre à jour. Avec [Homebrew](https://brew.sh), sur Mac ou Linux :

```bash
brew install ruwad-io/tap/askmywa
askmywa install-service
askmywa link
```

Pour utiliser un Pi depuis Claude Desktop sur votre ordinateur, voir le [README principal](README.md#on-a-raspberry-pi-or-another-always-on-machine) (en anglais).

## Mode serveur : envoi seul, porte HTTP privée et webhooks

Cette partie s'adresse aux développeurs. askmywa peut aussi tourner sur un
serveur pour **une application de confiance à vous** : par exemple une
application d'assistant personnel qui prépare des réponses et vous laisse les
envoyer en un clic. Trois options, toutes coupées par défaut, en font un
second appareil lié qui ne garde rien et ne fait que ce dont l'application a
besoin. La page [askmywa.com/fr/serveur](https://askmywa.com/fr/serveur/)
reprend tout cela pas à pas.

**Envoi seul.** `--send-only` (ou `ASKMYWA_SEND_ONLY=1`) ne garde **aucun
message** : ni message reçu, ni historique, ni copie de ce qui part. Seuls la
session WhatsApp et les noms des contacts sont stockés (et les noms des
groupes). L'option refuse `--legacy-store` : il n'y a pas d'historique où
importer.

**La porte HTTP.** `--http-addr ADRESSE` (ou `ASKMYWA_HTTP_ADDR`) ouvre trois
opérations, en HTTP simple. Le secret se lit dans `ASKMYWA_HTTP_TOKEN`
seulement (32 caractères ou plus, jamais en option de ligne de commande, pour
qu'il n'apparaisse pas dans `ps`) ; chaque requête porte
`Authorization: Bearer <secret>`.

| Requête | Réponse |
|---|---|
| `GET /v1/status` | `{"connected": true, "logged_in": true, "send_only": true}` |
| `GET /v1/contact?phone=%2B33600000001` | `{"name": "…"}` : le nom que **vous** avez donné à ce numéro dans votre carnet d'adresses, ou `null`. Un nom de profil ou d'entreprise est choisi par l'autre personne et ne garantit jamais un numéro. |
| `POST /v1/send` avec `{"to": "+33600000001", "text": "…", "idempotency_key": "…"}` | `{"message_id": "…", "timestamp": "…"}` : un texte à une personne ; ni groupe, ni fichier, rien d'autre dans le corps. Le texte part tel quel (4 000 caractères au plus). |

```bash
export ASKMYWA_HTTP_TOKEN=…   # le même secret que celui de l'application
curl -s http://askmywa.internal:8080/v1/status -H "Authorization: Bearer $ASKMYWA_HTTP_TOKEN"
curl -s -G http://askmywa.internal:8080/v1/contact --data-urlencode 'phone=+33600000001' \
  -H "Authorization: Bearer $ASKMYWA_HTTP_TOKEN"
curl -s http://askmywa.internal:8080/v1/send -H "Authorization: Bearer $ASKMYWA_HTTP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"to":"+33600000001","text":"Je serai là à 19 h.","idempotency_key":"brouillon-0001"}'
```

Encodez le `+` dans l'adresse (`%2B`, ou `curl -G --data-urlencode`) : un `+`
nu se lit comme une espace et le numéro est refusé.

- **Pas d'aperçu à la porte.** `POST /v1/send` envoie tout de suite. L'aperçu
  et l'accord reviennent à l'application qui l'appelle : elle vous montre le
  destinataire et le texte exact, et n'appelle la porte qu'après votre clic.
- **Une même clé ne part jamais deux fois**, même après un envoi en échec ou
  coupé : WhatsApp a pu le prendre, l'issue est donc inconnue et la porte
  répond 409 au lieu de réessayer. Clés et compteurs vivent en mémoire pendant
  24 heures ; un redémarrage les oublie.
- **Plafonds.** Au plus `ASKMYWA_HTTP_SENDS_PER_MINUTE` (5 par défaut) et
  `ASKMYWA_HTTP_SENDS_PER_DAY` (40 par défaut) envois ; au-delà, la porte
  répond 429 et n'envoie rien. Pas connecté : 503, rien n'est parti. WhatsApp
  n'a pas confirmé : 502, et la clé est consommée.
- **Réseau privé seulement.** La porte n'a pas de TLS et ne choisit pas qui
  peut la joindre : ne donnez au service **aucun domaine public** et
  joignez-le sur un réseau privé. Sans `--send-only`, elle prévient dans le
  journal que ce serveur garde aussi l'archive des messages.

**Webhooks.** `--webhook-url URL` (ou `ASKMYWA_WEBHOOK_URL`) envoie chaque
message *en direct* (pas l'historique), y compris ceux que vous envoyez depuis
votre téléphone, à une autre application, en JSON : `type` (`"message"`),
`id`, `timestamp`, `from_me`, `chat` (`jid`, `name`, `is_group`), `sender`
(`phone`, `lid`, `name`, `name_source`), `text` et, pour une pièce jointe, son
type `media`, son nom de fichier et son type MIME (jamais son contenu). Le
secret vient de `ASKMYWA_WEBHOOK_SECRET` seulement (32 caractères ou plus) ;
l'URL doit être en http(s), sans identifiants. Chaque requête porte
`X-Askmywa-Event-Id`, `X-Askmywa-Timestamp` (secondes Unix) et
`X-Askmywa-Signature: sha256=hex(HMAC-SHA256(secret, timestamp + "." + corps))`.
Vérifiez-la sur le corps brut :

```python
import hashlib, hmac, time

def verify(secret: str, headers: dict, body: bytes) -> bool:
    ts = headers.get("X-Askmywa-Timestamp", "")
    sig = headers.get("X-Askmywa-Signature", "")
    if not ts.isdigit() or abs(time.time() - int(ts)) > 300:
        return False  # plus de cinq minutes : refuser
    mac = hmac.new(secret.encode(), ts.encode() + b"." + body, hashlib.sha256)
    return hmac.compare_digest(sig, "sha256=" + mac.hexdigest())
```

Ignorez ensuite un identifiant d'événement déjà reçu. Les événements partent
dans l'ordre et sont réessayés avec un délai croissant (1 seconde, doublé
jusqu'à environ 4 minutes) jusqu'à une réponse 2xx ; une réponse 4xx autre que
408 ou 429 abandonne l'événement. La file vit en mémoire (2 000 événements,
les plus anciens tombent au-delà) : un redémarrage perd ce qui n'était pas
encore livré. Fonctionne avec `--send-only` : le message atteint le webhook et
askmywa ne garde rien.

**Plusieurs numéros.** En mode serveur avec `--send-only --pair wait`,
le compte `primary` conserve sa base de session existante. Pour un autre
numéro, l’application choisit un UUIDv4 et appelle
`POST /v1/accounts/{id}/pair`. Elle lit ensuite `GET /v1/accounts/{id}/pair`.
À l’état `qr`, elle affiche le PNG en base64 du champ `qr` jusqu’à
`expires_at`. À l’état `connected`, `phone` donne le numéro associé.
L’affichage est réservé au propriétaire connecté, sans cache ni journal du QR.
L’état, le carnet et l’envoi du compte passent par `/v1/accounts/{id}/`.
Les routes historiques `/v1/` gardent le compte principal. Le webhook signé
d’un compte supplémentaire porte le type `account.message` et son
`account_id`. Dédoublonnez par compte et identifiant de message. Conservez
tout le volume, répertoire `accounts/` compris, entre deux redémarrages.

**Docker.** `whatsapp-go/Dockerfile` construit le serveur en envoi seul :
données sur le volume `/data`, porte sur `[::]:8080`, et un point d'entrée qui
donne le volume à un utilisateur `askmywa` sans privilèges puis abandonne
root. Posez `ASKMYWA_HTTP_TOKEN` (et les variables du webhook si vous le
voulez) sur le service. Liez-le une fois depuis un terminal dans le conteneur
avec `askmywa link --phone <votre numéro>`, puis tapez le code dans WhatsApp.

> **Risque de bannissement.** Un appareil lié sur une adresse de centre de
> données n'est pas ce que WhatsApp attend, et ses conditions n'autorisent pas
> les clients non officiels. Gardez un faible volume et des plafonds serrés.
> Retirer l'appareil depuis votre téléphone (Appareils connectés) le coupe
> aussitôt.

## Pour aller plus loin

Le projet est libre et gratuit (licence MIT). Les explications techniques, l'installation sans l'extension et le développement sont en anglais dans [whatsapp-go/README.md](whatsapp-go/README.md).

Il est né d'une réécriture de [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), de Luke Harries.
