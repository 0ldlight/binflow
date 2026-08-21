# BinFlow nginx configuration

This directory contains nginx configuration templates for production deployments.

## Files

### ssl.conf.template

SSL termination template with TLS 1.2+ and Mozilla intermediate ciphers.
Fill in the placeholders (\${DOMAIN}, \${SSL_CERT}, \${SSL_KEY},
\${BINFLOW_UPSTREAM}) before deployment.

Quick start with certbot (Let's Encrypt):

1. `sudo certbot certonly --nginx -d registry.example.com`
2. cp ssl.conf.template /etc/nginx/conf.d/binflow-ssl.conf
3. Edit the placeholders:
   - DOMAIN=registry.example.com
   - SSL_CERT=/etc/letsencrypt/live/registry.example.com/fullchain.pem
   - SSL_KEY=/etc/letsencrypt/live/registry.example.com/privkey.pem
4. `sudo nginx -t && sudo systemctl reload nginx`

For docker-compose deployments, see deploy/compose/docker-compose.yml and
deploy/compose/nginx.conf (the compose-native proxy config).