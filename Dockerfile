FROM cloudflare/cloudflared:2023.5.0

COPY ./neckless /bin/neckless

ENTRYPOINT ["neckless"]
