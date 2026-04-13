<p align="center">
<a href="https://seanime.app/">
<img src="docs/images/seanime-logo.png" alt="preview" width="70px"/>
</a>
</p>

<h1 align="center"><b>Seanime</b></h1>

<p align="center">
<img src="https://seanime.app/bucket/gh-showcase.webp" alt="preview" width="100%"/>
</p>

<p align="center">
  <a href="https://seanime.app/docs">Documentation</a> |
  <a href="https://github.com/5rahim/seanime/releases">Latest release</a> |
  <a href="https://www.youtube.com/playlist?list=PLgQO-Ih6JClhFFdEVuNQJejyX_8iH82gl">Tutorials</a> |
  <a href="https://discord.gg/Sbr7Phzt6m">Discord</a> |
  <a href="https://seanime.app/docs/policies">Copyright</a>
</p>

<div align="center">
  <a href="https://github.com/5rahim/seanime/releases">
    <img src="https://img.shields.io/github/v/release/5rahim/seanime?style=flat-square&color=blue" alt="" />
  </a>
  <a href="https://github.com/5rahim/seanime/releases">
    <img src="https://img.shields.io/github/downloads/5rahim/seanime/total?style=flat-square&color=blue" alt="" />
  </a>
	<a href="https://discord.gg/Aruz7wdAaf">
	  <img src="https://img.shields.io/discord/1224767201551192224?style=flat-square&logo=Discord&color=blue&label=Discord" alt="discord">
	</a>
  <a href="https://github.com/sponsors/5rahim">
    <img src="https://img.shields.io/static/v1?label=Sponsor&style=flat-square&message=%E2%9D%A4&logo=GitHub&color=%23fe8e86" alt="" />
  </a>
</div>


<h5 align="center">
Leave a star if you like the project! ⭐️
</h5>

## About

Seanime is a **media server** with a **web interface** and **desktop app** for managing your local library, streaming anime and reading manga.

> [!IMPORTANT]
>Seanime does not provide, host, or distribute any media content. Users are responsible for obtaining media through legal means and complying with their local laws. Extensions listed on the app are unaffiliated with Seanime and may be removed if they violated copyright laws. </strong>


## What's New

- **Multi-User Profiles**: Netflix-style profile picker with per-user AniList accounts, independent settings (library, torrent, debrid, mediastream), and concurrent streaming sessions. Admin registration on first launch with an optional household access code.
- **Debrid Integration**: Stream via Real-Debrid, AllDebrid, TorBox, or StremThru with background downloading, estimated keyframes for fast HLS start, and automatic local file switchover for smooth playback.
- **Docker Support**: Official Docker images (default, rootless, VA-API hardware-accelerated, CUDA) published to GitHub Container Registry with built-in qBittorrent.

## Features

- **Cross-platform**: Web interface and desktop app for Windows, Linux, and macOS
- **Seanime Denshi**: Desktop client with built-in video player (support for SSA/ASS subtitles, Anime4K sharpening, auto translation, and more)
- **AniList Integration**: Browse and manage your lists, discover anime and manga
- **Custom Sources**: Support for adding non-AniList anime and manga series 
- **Library Management**: Fast and smart scanning of local files without strict naming conventions or folder structures
- **Torrent Integration**: Built-in torrent search engine via extensions and downloading support with Qbittorrent, Transmission, Torbox, and Real-Debrid
- **Torrent Streaming**: Stream torrents directly to the media player without waiting for downloads (supports Bittorrent, Torbox and Real-Debrid)
- **Online Streaming**: Watch anime from online sources directly within the app via extensions
- **Auto Downloader**: Automatically track and download new episodes with customizable filters and advanced features (prioritization, scoring, delay, etc.)
- **Extension Marketplace**: In-app repository to install and manage extensions for online streaming, manga sources, and torrent providers
- **Manga Reader**: Read chapters from your local library or via extensions with a unified interface
- **Transcoding & Direct Play**: Stream your library to any device web browser with on-the-fly transcoding or direct play
- **External Player Support**: Seamless integration with MPV, VLC, and MPC-HC on desktop
- **Mobile Player Integration**: Open files and streams in mobile players (Outplayer, VLC, etc.) via intents or deep links
- **Playlists**: Create and manage playlists for a seamless binge watching experience
- **Customizable UI**: Personalize the interface with color themes, background images, and layout options
- **Discord Rich Presence**: Display your watching activity automatically
- **Offline Mode**: Access your anime and manga library without an internet connection
- **Schedule & Season Browser**: Track upcoming releases and missed episodes, browse current/previous/next season anime with filters and sorting

## Get started

Read the installation guide to set up Seanime on your device.

<p align="center">
<a href="https://seanime.app/docs" style="font-size:18px;" align="center">
How to install Seanime
</a>
</p>

### Docker Compose

Seanime publishes Docker images to GitHub Container Registry. Four variants are available:

| Tag | Description |
|-----|-------------|
| `latest` | Default image (runs as root) |
| `rootless` | Runs as non-root user (UID/GID 1000) |
| `hwaccel` | Rootless + VA-API hardware-accelerated transcoding |
| `cuda` | NVIDIA CUDA hardware-accelerated transcoding |

**1. Create a `docker-compose.yml`**

```yaml
services:
  seanime:
    image: ghcr.io/awu05/seanime:rootless # or :latest, :hwaccel, :cuda
    container_name: seanime
    environment:
      - QBIT_WEBUI_PORT=8081
      - QBIT_USERNAME=admin
      - QBIT_PASSWORD=adminadmin
      # Bootstrap admin account (optional — skip to use the web setup page)
      - SEANIME_ADMIN_USERNAME=admin
      - SEANIME_ADMIN_PASSWORD=changeme
      - SEANIME_INSTANCE_ACCESS_CODE=1234
    ports:
      - "3211:43211" # Seanime Web UI
      - "8081:8081"  # qBittorrent Web UI
    user: "1000:1000" # omit for :latest (root) variant
    volumes:
      - ./seanime-data/config:/home/seanime/.config # use /root/.config for :latest
      - ./seanime-data/anime:/anime
      - ./seanime-data/downloads:/downloads
    restart: unless-stopped
```

> For hardware acceleration, add `devices: [/dev/dri:/dev/dri]` and `group_add: [video, render]` for VA-API,
> or `runtime: nvidia` and the `NVIDIA_VISIBLE_DEVICES`/`NVIDIA_DRIVER_CAPABILITIES` environment variables for CUDA.
> See the examples in [`docker/examples/`](docker/examples/) for full configurations.

**2. Start the container**

```bash
docker compose up -d
```

**3. Open the Web UI**

Navigate to `http://localhost:3211`. On first launch you will be prompted to create an admin account.

**Environment variables**

| Variable | Description |
|----------|-------------|
| `SEANIME_ADMIN_USERNAME` | Admin username created on first start |
| `SEANIME_ADMIN_PASSWORD` | Admin password created on first start |
| `SEANIME_INSTANCE_ACCESS_CODE` | Access code for household members to create profiles |
| `QBIT_WEBUI_PORT` | qBittorrent WebUI port (default `8081`) |
| `QBIT_USERNAME` | qBittorrent username |
| `QBIT_PASSWORD` | qBittorrent password |

<br>

## Goal

This is a one-person project and may not meet every use case. If it doesn’t fully fit your needs, other tools might be a better match.

### Not planned

- Android, iOS, AndroidTV, tvOS, ... apps
- Built-in support for other trackers such as MyAnimeList, Trakt, SIMKL, etc.
- Built-in support for other media players
- Built-in localization (translations)


## Tech stack

* Server: [Go](https://go.dev/)
* Frontend: [React](https://reactjs.org/), [Rsbuild/Rspack](https://rsbuild.rs/), [Tanstack Router](https://tanstack.com/router)
* Seanime Denshi: [Electron](https://www.electronjs.org/)

## Development and Build

Building from source is straightforward, you'll need [Node.js](https://nodejs.org/en/download) and [Go](https://go.dev/doc/install) installed on your system.
Development and testing might require additional configuration.

[Read more here](https://github.com/5rahim/seanime/blob/main/DEVELOPMENT_AND_BUILD.md)

<br>

<br>

> [!NOTE]
> For copyright-related requests, please contact the maintainer using the contact information provided on [the website](https://seanime.app/docs/policies).
