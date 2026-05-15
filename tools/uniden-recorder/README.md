# Uniden BCD996XT companion recorder

A small Python daemon that turns a USB-connected **Uniden BCD996XT** (and
close relatives like the BCD436HP / BCD536HP) into a continuous feed for
rdio-scanner. It polls the scanner over USB serial for "what am I hearing
right now?" metadata, captures the audio from a USB sound card line-in, and
drops finished calls into a directory that rdio-scanner's **Dir Watch**
ingests automatically.

This is purpose-built; there is no trunk-recorder / SDR involved. The
scanner does the demod; this just writes the audio file with the right name.

## What you need

| Item                                | Notes                                       |
| ----------------------------------- | ------------------------------------------- |
| Raspberry Pi (3B+ or newer)          | A Pi 4 is comfortable; a Pi Zero 2 will work |
| Uniden BCD996XT (or BCD436/536HP)    | Most Uniden DMA-era scanners speak `GLG`    |
| USB A-to-mini cable                  | For the scanner's USB serial port           |
| USB audio dongle with line-in        | Pi onboard audio has *no* input             |
| 3.5 mm cable scanner REC OUT → dongle | Use REC OUT, not headphone, so the level is constant |
| rdio-scanner already running         | Same Pi is fine                             |

## Quick start

```sh
# 1. Install runtime deps
sudo apt-get update
sudo apt-get install -y python3-pip libportaudio2

# 2. Install Python libraries. On modern Pi OS you'll need --break-system-packages
#    (or use a venv) because of PEP 668.
pip3 install --break-system-packages pyserial sounddevice numpy

# 3. Discover hardware identifiers
cd /home/pi/rdio-scanner/tools/uniden-recorder
python3 uniden_recorder.py --list-serial
python3 uniden_recorder.py --list-audio

# 4. Configure
cp uniden-recorder.example.ini uniden-recorder.ini
nano uniden-recorder.ini   # set serial_port and audio_device

# 5. Smoke test with verbose logging
python3 uniden_recorder.py -c uniden-recorder.ini -v

# 6. Install as a service
sudo cp uniden-recorder.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now uniden-recorder
sudo journalctl -u uniden-recorder -f
```

## Wiring it up in rdio-scanner

In the admin panel:

1. Go to **Systems** and create the system you want calls to land in (or
   enable **Auto-populate** on an existing system so new talkgroups get
   created automatically).
2. Go to **Dir Watch** and add a new entry:

   | Field            | Value                                           |
   | ---------------- | ----------------------------------------------- |
   | Disabled         | off                                             |
   | Delete After     | on (otherwise the dir fills forever)            |
   | Type             | **Default**                                     |
   | Directory        | `/home/pi/scanner-calls` (or whatever `output_dir` is) |
   | Extension        | `wav`                                           |
   | System           | the system you created                          |
   | Talkgroup        | leave empty (parsed from the filename)          |
   | Mask             | `#DATE_#TIME_#SYSLBL_#TGLBL_#HZ.wav`            |
   | Frequency        | leave empty (parsed from the filename)          |
   | Delay            | 2000                                            |

   Save. The system needs **Auto-populate** ON if you want new talkgroups
   created on demand from `#TGLBL`.

The mask uses `#SYSLBL` / `#TGLBL` (the display *names*), not `#SYS` / `#TG`
(the numeric IDs), because the scanner's `GLG` reply gives us names.

## How it works (one screen of detail)

Two threads + the audio callback:

- **Audio callback** (PortAudio): pushes 16-bit PCM frames into a small ring
  buffer (~500 ms by default).
- **Audio consumer**: drains the queue, keeps the ring full, and if a call
  is open, writes the same frames to the WAV.
- **Serial loop**: every ~200 ms sends `GLG\r`, parses the comma-separated
  reply, decides whether squelch is open and on which system/talkgroup.

When squelch opens, we open a `.part` file, prepend the pre-roll, and start
streaming. When squelch stays closed for `min_silence_ms`, we finalise:
close the wave header, rename `foo.wav.part` → `foo.wav` atomically. That
final rename is what trips DirWatch's fsnotify.

If the talkgroup or frequency changes while squelch is still open, the
current file is finalised and a new one opens immediately — so back-to-back
transmissions on different TGs don't get glued together.

## Troubleshooting

**No serial reply / `GLG <- ""` in verbose mode.** Wrong port or wrong
baud. Try 38400, 57600, or check what `--list-serial` shows. If the device
shows up as `/dev/ttyACM0` instead of `/dev/ttyUSB0`, change `serial_port`
to match.

**Metadata fields are off by one** (system label appears as the group
name, etc.). Different Uniden firmware revisions shuffle the GLG field
positions. Run with `-v`, eyeball a few real `GLG <- ...` lines, and adjust
`_field()` indices in `GlgState.parse()`. The header comment at the top of
the file documents the BCD996XT layout we assume.

**Calls split into 5 short files.** Increase `min_silence_ms` (try 3000).
The transmitter is keying off briefly between words.

**Calls glued together with multiple speakers.** Decrease `min_silence_ms`
(try 800). Now you're holding open across the gap between two operators.

**`audio_device 'USB Audio' not found`.** `--list-audio` to see what name
the dongle actually advertises, then put a substring of it in the ini. Case
matters.

**`Permission denied: '/dev/ttyUSB0'`.** Add `pi` to the `dialout` group:

```sh
sudo usermod -aG dialout pi
# log out and back in
```

**Wave files are huge.** Drop `samplerate` to 16000 or 11025; FM voice
fits in either fine, and DirWatch will transcode anyway.

**Auto-populate isn't creating talkgroups.** First, confirm Auto-populate
is ON for the system the DirWatch is pointing at (Admin → Systems → that
system → Auto-populate slider). Without it, rdio-scanner refuses to create
talkgroups it hasn't seen before. Second, the talkgroup label must be
short and not contain characters that confuse the mask parser. The script
strips anything outside `[A-Za-z0-9.-]` to **dashes** (not underscores —
`_` is reserved as the mask field separator), and labels are truncated to
48 chars. So `"Decatur County Fire EMS"` becomes `Decatur-County-Fire-EMS`
in the filename, which the mask parses unambiguously.

## Admin-managed config (optional)

If you'd rather not SSH into the Pi to tweak silence thresholds, register
the recorder in the rdio-scanner admin UI:

1. **Admin → Recorders → New recorder.** Give it a label, copy the
   generated **API key**, set System / Output directory / Min silence /
   Pre-roll, save.
2. On the Pi, add three lines to `uniden-recorder.ini`:

   ```ini
   server_url = http://your-rdio-scanner-host:3000
   api_key    = <paste the key from the admin UI>
   refresh_seconds = 30
   ```
3. Restart the service. The daemon will now pull soft settings (enabled
   flag, output dir, silence, pre-roll) from the server every 30 s.

Hardware-local settings (`serial_port`, `audio_device`, `samplerate`,
`channels`) **are not** remotely tunable — only the recorder's local
operator knows what's plugged in where, so changing those over the wire
would just be a footgun.

**Security note.** The `api_key` rides in plain `Authorization: Bearer`
headers. Over `http://` anyone on the same network can sniff it and
impersonate the recorder. Use `https://` (the rdio-scanner server supports
SSL certs and ACME via the `ssl_*` options) unless the recorder and the
server are on the same trusted LAN.

## Limitations

- GLG field positions are best-effort and BCD996XT-tuned. Adjacent models
  mostly match, but verify with `-v` on first run.
- P25 unit ID capture isn't implemented; `GLG` doesn't expose it. If you
  need per-unit attribution, you'd need to also poll `PSI` and correlate,
  which this daemon doesn't do.
- Multi-instance is unsupported by design. One scanner, one recorder.
