<img src="whatsapp-go/assets/logo.svg" width="96" height="96" alt="askmywa logo">

# askmywa — WhatsApp pour Claude

**[askmywa.com](https://askmywa.com/fr/)** · *[English version](README.md)* · [Politique de confidentialité](https://askmywa.com/fr/confidentialite/)

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

## Pour aller plus loin

Le projet est libre et gratuit (licence MIT). Les explications techniques, l'installation sans l'extension et le développement sont en anglais dans [whatsapp-go/README.md](whatsapp-go/README.md).

Il est né d'une réécriture de [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), de Luke Harries.
