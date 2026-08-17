# hoerbox

A whitelisted YouTube Music fetcher for a kid: it downloads a curated set
of playlists/tracks into a local library, laid out for Hörbert-style
kids' audio players to play from. It's a fetcher, not a player — a
Raspberry Pi and physical triggers (buttons/RFID) would be a separate,
later project, not something this repo does.

## Install (macOS)

```sh
brew install go yt-dlp ffmpeg   # + python3, but macOS already has that
git clone <this repo> && cd hoerbox
make install
```

Then double-click `hoerbox.app` in `~/Applications`. If something's
missing or wrong, hoerbox says so in a dialog rather than doing nothing.

**First thing, before adding anything**: click **Browser** and pick the
browser you're logged into YouTube Music with. Without matching cookies,
most real tracks fail to download — this isn't optional setup.

## Using it

- **Browser** picks which local browser hoerbox reads YouTube cookies
  from (see above — get this right first).
- **Add** a YouTube Music playlist/album URL — hoerbox starts downloading
  it in the background automatically. Downloads run only while the app is
  open, and pace themselves deliberately (not a bug — see below).
- The table shows every track's status; click a column to sort.
- **Remove Done** clears out playlists that finished completely.
- **Open Folder** shows the downloaded files.

Your library lives at `~/Music/hoerbox` (`in/` for what to fetch, `out/`
for the results) regardless of where the app icon itself sits.

Downloads are paced (never starting a new one within half the previous
track's own runtime) to avoid looking like a scraper — a big playlist
taking a while to fill in is expected, not stuck.

## CLI

The same binary is also a full CLI — `bin/hoerbox --help` for every
subcommand (`add`, `stream`, `list`, `stats`, `clean`, `repair`, ...).

## Status

The library/download pipeline and desktop GUI work; that's all this repo
is right now. Actual playback — a Raspberry Pi, physical triggers — is a
future project, not started here.

## Development

```sh
make check      # go vet + gofmt check + brokkr lint
make help       # list all targets
```

Requires Go 1.26+. Originally built on Spotify (go-librespot); switched
to YouTube Music after Spotify's DRM changes made that a dead end.

## License

[MIT](LICENSE)

## Disclaimer

hoerbox downloads audio via `yt-dlp`, which isn't hoerbox's call to make
legal for you — that depends on your jurisdiction, YouTube's terms of
service, and what you're downloading. It's on you to make sure your use
is actually allowed where you are before running this. Built for
personal, non-commercial use only; no warranty, use at your own risk.
