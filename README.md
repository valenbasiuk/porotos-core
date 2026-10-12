# Porotos 2 Core

Mod de Geode para **Porotos 2 GDPS** (GD 2.2081, Windows y Android).

- Redirige todas las requests de `boomlings.com/database` al server de Porotos 2 (URL configurable en los ajustes del mod).
- El botón **Play** abre los niveles online; los niveles main y The Tower quedan bloqueados.
- Al completar un nivel online, manda el puntaje al server en el momento, así el server suma las estrellas o lunas.

Las estrellas, lunas, demons y monedas **las calcula el server**, no el juego. Aunque alguien juegue con el GD normal y GDPS Switcher (sin este mod) y tenga los main pasados en su save, esos main valen 0.
Sin el mod, para que cuente un nivel online hay que abrir su leaderboard una vez después de pasarlo.

## Compilar

Lo compila GitHub Actions (`.github/workflows/build.yml`) en cada push. El `.geode` queda en los artifacts del run.

Local (Windows): instalar el [Geode SDK y CLI](https://docs.geode-sdk.org/getting-started/geode-cli), luego:

```
geode build
```
