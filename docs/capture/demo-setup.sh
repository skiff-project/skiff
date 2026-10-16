#!/usr/bin/env bash
# Builds the demo data used by the documentation screenshots:
#  - a fake home directory for the local panel ($1, default ./demo)
#  - content on the Docker test servers (testenv/docker-compose.yml)
set -euo pipefail
DEMO="${1:-$(pwd)/demo}"
HOME_DIR="$DEMO/home/alex"
ICON="$(cd "$(dirname "$0")/../.." && pwd)/build/appicon.png"

rm -rf "$DEMO"
mkdir -p "$HOME_DIR"/{Projets/site-vitrine/{css,js,images},Documents/factures,Images,Téléchargements,Bureau,.ssh}

# ---- local website project ----------------------------------------------
SITE="$HOME_DIR/Projets/site-vitrine"
cat > "$SITE/index.html" <<'EOF'
<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <title>Atelier Lumière — Accueil</title>
  <link rel="stylesheet" href="css/style.css">
</head>
<body>
  <header class="hero">
    <img src="images/logo.png" alt="Logo">
    <h1>Bienvenue à l'Atelier Lumière</h1>
    <p>Luminaires artisanaux fabriqués à la main depuis 1998.</p>
    <a class="button" href="contact.html">Nous contacter</a>
  </header>
  <script src="js/app.js"></script>
</body>
</html>
EOF
cat > "$SITE/contact.html" <<'EOF'
<!doctype html>
<html lang="fr">
<head><meta charset="utf-8"><title>Contact</title><link rel="stylesheet" href="css/style.css"></head>
<body>
  <h1>Contact</h1>
  <form action="/send" method="post">
    <label>Nom <input name="name" required></label>
    <label>Message <textarea name="message"></textarea></label>
    <button>Envoyer</button>
  </form>
</body>
</html>
EOF
cat > "$SITE/a-propos.html" <<'EOF'
<!doctype html>
<html lang="fr"><head><meta charset="utf-8"><title>À propos</title></head>
<body><h1>Notre histoire</h1><p>Un atelier familial, trois générations de passionnés.</p></body></html>
EOF
cat > "$SITE/css/style.css" <<'EOF'
:root { --brand: #e8a33d; --ink: #1d1d1f; }
body { margin: 0; font-family: system-ui, sans-serif; color: var(--ink); }
.hero { padding: 96px 24px; text-align: center; background: linear-gradient(135deg, #fff7e6, #ffe0b3); }
.hero h1 { font-size: 2.6rem; margin: 16px 0 8px; }
.button { display: inline-block; padding: 12px 22px; border-radius: 999px; background: var(--brand); color: #fff; text-decoration: none; }
EOF
cat > "$SITE/js/app.js" <<'EOF'
// Small enhancements for the showcase site.
document.addEventListener('DOMContentLoaded', () => {
  const year = new Date().getFullYear()
  document.querySelectorAll('[data-year]').forEach((el) => (el.textContent = String(year)))
  console.log('Atelier Lumière — site chargé')
})
EOF
cat > "$SITE/README.md" <<'EOF'
# Site vitrine — Atelier Lumière

Site statique déployé sur le serveur de production avec **Skiff**.

## Déploiement

1. Ouvrir le site *Production → Site vitrine (SFTP)*.
2. `Ctrl+S` pour synchroniser `site-vitrine` vers `/config/www`.
3. Vérifier l'aperçu, puis lancer la synchronisation.

| Environnement | Dossier |
|---|---|
| Production | `/config/www` |
| Recette | `/srv/recette` |
EOF
printf 'User-agent: *\nAllow: /\nSitemap: https://www.example.com/sitemap.xml\n' > "$SITE/robots.txt"
printf 'RewriteEngine On\nRewriteRule ^accueil$ index.html [L]\n' > "$SITE/.htaccess"
printf '#!/bin/sh\n# Déploiement en une commande\nskiff run -c "open Production/Site vitrine (SFTP)" -c "synchronize remote . /config/www" -c exit\n' > "$SITE/deploy.sh"
chmod +x "$SITE/deploy.sh"
printf '{\n  "name": "site-vitrine",\n  "version": "2.4.0",\n  "private": true\n}\n' > "$SITE/package.json"
cp "$ICON" "$SITE/images/logo.png"
python3 - "$ICON" "$SITE/images" "$HOME_DIR/Images" <<'PY'
import sys
from PIL import Image, ImageDraw
icon, site_images, pictures = sys.argv[1], sys.argv[2], sys.argv[3]
def scene(path, size, c1, c2, sun=True):
    w, h = size
    img = Image.new("RGB", size)
    px = img.load()
    for y in range(h):
        t = y / h
        col = tuple(int(c1[i] + (c2[i] - c1[i]) * t) for i in range(3))
        for x in range(w):
            px[x, y] = col
    d = ImageDraw.Draw(img)
    if sun:
        d.ellipse([w*0.62, h*0.18, w*0.82, h*0.18 + w*0.2], fill=(255, 214, 120))
    d.polygon([(0, h), (w*0.35, h*0.55), (w*0.6, h*0.8), (w*0.8, h*0.5), (w, h*0.75), (w, h)], fill=(46, 64, 87))
    img.save(path)
scene(f"{site_images}/banniere.png", (1200, 500), (255, 196, 120), (255, 240, 220))
scene(f"{site_images}/atelier.jpg", (900, 600), (120, 170, 220), (230, 240, 250))
scene(f"{pictures}/vacances-bretagne.png", (1024, 683), (140, 190, 235), (240, 245, 250))
scene(f"{pictures}/coucher-de-soleil.jpg", (1024, 683), (250, 120, 90), (255, 210, 150))
PY

# ---- documents & downloads -----------------------------------------------
printf '%%PDF-1.4\n%% Rapport annuel 2026 (document de démonstration)\n' > "$HOME_DIR/Documents/rapport-annuel-2026.pdf"
for m in 07 08 09; do printf '%%PDF-1.4\n%% Facture 2026-%s\n' "$m" > "$HOME_DIR/Documents/factures/facture-2026-$m.pdf"; done
printf 'Nom;Quantité;Prix\nSuspension laiton;12;189\nLampe de bureau;30;79\n' > "$HOME_DIR/Documents/inventaire.csv"
printf '# Notes\n\n- Renouveler le certificat TLS en novembre\n- Migrer les sauvegardes vers S3\n' > "$HOME_DIR/Documents/notes.md"
head -c 32000000 /dev/urandom > "$HOME_DIR/Téléchargements/sauvegarde-photos.tar.gz"
head -c 2400000 /dev/urandom > "$HOME_DIR/Téléchargements/catalogue-2027.zip"

# ---- SSH config (OpenSSH hosts and import demo) ---------------------------
cat > "$HOME_DIR/.ssh/config" <<'EOF'
Host bastion
    HostName bastion.example.com
    User alex
    IdentityFile ~/.ssh/id_ed25519

Host recette
    HostName 10.10.0.21
    User deploy
    ProxyJump bastion
    LocalForward 5432 db.internal:5432
    ForwardAgent yes

Host nas
    HostName 192.168.1.20
    User alex
    Port 2200

Host *
    ServerAliveInterval 20
    IdentityFile ~/.ssh/id_ed25519
    AddKeysToAgent yes
EOF

# ---- demo containers (Containers dialog) ---------------------------------
# a Compose-like project (its label groups it) with a stopped container
for c in boutique-web boutique-redis boutique-worker; do
  docker rm -f "$c" >/dev/null 2>&1 || true
done
docker run -d --name boutique-web -p 127.0.0.1:18080:80 --label com.docker.compose.project=boutique --entrypoint sleep rclone/rclone:latest 604800 >/dev/null
docker run -d --name boutique-redis --label com.docker.compose.project=boutique --entrypoint sleep rclone/rclone:latest 604800 >/dev/null
docker run -d --name boutique-worker --label com.docker.compose.project=boutique --entrypoint sleep rclone/rclone:latest 604800 >/dev/null
docker stop -t 0 boutique-worker >/dev/null

# varied timestamps
touch -d "2026-09-28 10:12" "$SITE/a-propos.html" "$SITE/robots.txt" "$SITE/.htaccess"
touch -d "2026-10-02 18:40" "$SITE/css/style.css" "$SITE/js/app.js" "$SITE/images/"*
touch -d "2026-10-14 09:05" "$SITE/index.html" "$SITE/README.md"
touch -d "2026-10-15 07:05" "$SITE/contact.html"

# ---- remote content (Docker test servers) --------------------------------
docker exec testenv-ssh-1 sh -c '
  cd /config && rm -rf www backups releases big.bin main.go notes.md photos clitest skiff-test-* skiff-sync.bin e2e-* ui-* 2>/dev/null
  mkdir -p www/css www/js www/images backups releases
  printf "<!doctype html><title>Atelier Lumière</title><h1>Version en ligne</h1>\n" > www/index.html
  printf "<!doctype html><title>Contact</title><h1>Contact</h1>\n" > www/contact.html
  printf "<!doctype html><title>À propos</title><h1>Notre histoire</h1>\n" > www/a-propos.html
  printf "<!doctype html><title>Ancienne page</title>\n" > www/promo-ete.html
  printf "body{margin:0}\n" > www/css/style.css
  printf "console.log(1)\n" > www/js/app.js
  printf "User-agent: *\nAllow: /\n" > www/robots.txt
  for i in $(seq 1 400); do
    c=200; p=/index.html
    [ $((i % 17)) = 0 ] && c=404 && p=/ancienne-page.html
    [ $((i % 53)) = 0 ] && c=500 && p=/contact.php
    echo "203.0.113.$((i % 250)) - - [15/Oct/2026:09:$((i % 60)):00 +0200] \"GET $p HTTP/2\" $c 5120"
  done > logs/access.log
  head -c 6000000 /dev/urandom > backups/site-2026-10-01.tar.gz
  head -c 5800000 /dev/urandom > backups/site-2026-09-01.tar.gz
  printf "services:\n  web:\n    image: nginx:stable\n    ports: [\"80:80\"]\n    volumes: [\"./www:/usr/share/nginx/html:ro\"]\n" > docker-compose.yml
  printf "# Serveur de production\n\nNe pas modifier www/ à la main : utiliser la synchronisation Skiff.\n" > LISEZMOI.md
  touch -d "2026-10-01 07:00" www/index.html www/css/style.css www/js/app.js www/robots.txt
  touch -d "2026-09-28 10:12" www/a-propos.html
  touch -d "2026-10-15 05:20" www/contact.html
  touch -d "2026-06-20 11:00" www/promo-ete.html
'
docker cp "$ICON" testenv-ssh-1:/config/www/images/logo.png
# systemd on the SSH server, for the services window (Alpine has none),
# and an internal web page on 127.0.0.1:3000 for the ports window
docker exec -i -u 0 testenv-ssh-1 sh -s install < "$(dirname "$0")/../../testenv/fake-systemd.sh"
docker exec -i -u skiff testenv-ssh-1 sh -s start < "$(dirname "$0")/../../testenv/fake-web.sh"

docker exec testenv-ssh-1 sh -c 'chown -R skiff:users /config/www /config/backups /config/releases /config/docker-compose.yml /config/LISEZMOI.md /config/logs/access.log 2>/dev/null; touch -d "2026-10-02 18:40" /config/www/images/logo.png'

docker exec testenv-ftp-1 sh -c '
  cd /home/skiff && rm -rf ./* 2>/dev/null
  mkdir -p livraisons/2026-10 commandes
  printf "Commande 1042 — 12 suspensions laiton\n" > commandes/commande-1042.txt
  printf "Commande 1043 — 30 lampes de bureau\n" > commandes/commande-1043.txt
  head -c 1500000 /dev/urandom > livraisons/2026-10/bon-de-livraison.pdf
  printf "Bienvenue sur le FTP partenaire.\n" > BIENVENUE.txt
  chown -R ftpuser:ftpgroup /home/skiff 2>/dev/null || true
'

docker exec testenv-webdav-1 sh -c '
  cd /data && rm -rf ./* 2>/dev/null
  mkdir -p Partagé/Photos Partagé/Contrats Personnel
  printf "Contrat de maintenance 2026\n" > Partagé/Contrats/maintenance-2026.txt
  printf "Liste de courses\n- ampoules E27\n" > Personnel/courses.md
'
docker cp "$ICON" testenv-webdav-1:/data/Partagé/Photos/logo.png

echo "demo ready in $DEMO"
