#!/usr/bin/env python3
# uniden_recorder.py - Companion daemon for Uniden BCD996XT (and family)
# scanners that exposes the currently broadcasting channel to rdio-scanner.
#
# It does two things in parallel:
#   1. Continuously captures audio from an input device (USB sound card line-in
#      fed from the scanner's REC OUT jack) into a small ring buffer.
#   2. Polls the scanner over USB serial with `GLG` at ~5 Hz to learn current
#      frequency / system tag / talkgroup tag / squelch state.
#
# When squelch opens, the daemon splices the pre-roll out of the ring buffer
# into a new WAV file and keeps appending live audio until squelch closes (or
# the talkgroup changes mid-stream). The final file is named so rdio-scanner's
# DirWatch with mask  #DATE_#TIME_#SYSLBL_#TGLBL_#HZ.wav  and
# Auto-populate ON can ingest it directly.
#
# License: GPL-3.0-or-later, same as rdio-scanner.

import argparse
import collections
import configparser
import logging
import os
import queue
import re
import signal
import sys
import threading
import time
import wave
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

try:
    import numpy as np
    import serial
    import sounddevice as sd
except ImportError as exc:
    sys.stderr.write(
        f"missing dependency: {exc.name}\n"
        "install with: pip3 install pyserial sounddevice numpy\n"
    )
    sys.exit(2)


LOG = logging.getLogger("uniden-recorder")

DEFAULT_CONFIG_PATHS = (
    "uniden-recorder.ini",
    "/etc/uniden-recorder.ini",
)

# Regex that strips characters that would be a pain in a filename. We keep
# alphanumerics, dash, underscore; everything else becomes underscore.
_FNAME_SAFE = re.compile(r"[^A-Za-z0-9._-]+")

# How long after squelch-close we wait before finalising the call, so a brief
# carrier drop in the middle of a transmission doesn't split it into two
# files. Tunable from the .ini as min_silence_ms.
DEFAULT_MIN_SILENCE_MS = 1500

# How much audio (per channel) we keep in the ring buffer so we can prepend
# the moment immediately before squelch opened to the WAV. PortAudio delivers
# in chunks of `blocksize` samples; the ring holds at least pre_roll_ms worth.
DEFAULT_PRE_ROLL_MS = 500


@dataclass
class GlgState:
    """Snapshot of a single GLG response."""
    raw: str
    frequency_hz: Optional[int]
    system_label: str
    talkgroup_label: str
    squelch_open: bool

    @classmethod
    def parse(cls, line: str) -> Optional["GlgState"]:
        # Typical reply on a BCD996XT firmware:
        #   GLG,<freq_or_tgid>,<mod>,<att>,<ctcss>,<name1>,<name2>,<name3>,<sql>,<mut>,<sys_tag>,<chan_tag>,<p25_nac>
        # Field positions drift between firmware revs; this is best-effort.
        # Run with -v to see raw replies and tweak _parse_fields() if needed.
        line = line.strip()
        if not line.startswith("GLG"):
            return None
        parts = line.split(",")
        if len(parts) < 9:
            return None

        # Frequency in Hz on conventional scan; on trunked it's a TGID. Either
        # way we treat it as the "#HZ" the mask is looking for, because the
        # rdio-scanner mask only uses it to display, not to identify.
        freq_hz: Optional[int] = None
        try:
            freq_hz = int(parts[1])
        except (ValueError, IndexError):
            freq_hz = None

        def _field(i: int) -> str:
            return parts[i].strip() if 0 <= i < len(parts) else ""

        system_label = _field(5)
        # parts[6] is "group" (e.g. site/dept); parts[7] is the channel/TG
        # name on conventional and trunked respectively. Prefer name3 if
        # present, else fall back to name2.
        talkgroup_label = _field(7) or _field(6)
        sql_raw = _field(8)
        squelch_open = sql_raw == "1"

        return cls(
            raw=line,
            frequency_hz=freq_hz,
            system_label=system_label,
            talkgroup_label=talkgroup_label,
            squelch_open=squelch_open,
        )

    def call_key(self) -> tuple:
        """Identity used to decide if we're still in the same call."""
        return (self.system_label, self.talkgroup_label, self.frequency_hz)


class AudioRing:
    """Lock-free-ish ring buffer of recent audio frames for pre-roll splicing."""

    def __init__(self, samplerate: int, channels: int, pre_roll_ms: int):
        self.samplerate = samplerate
        self.channels = channels
        # Hold ~2x pre-roll worth of blocks so we always have full pre_roll
        # available even if the producer is briefly ahead of the consumer.
        max_samples = int(samplerate * pre_roll_ms / 1000) * 2
        self._blocks: collections.deque = collections.deque()
        self._sample_count = 0
        self._max_samples = max_samples
        self._lock = threading.Lock()

    def push(self, block: np.ndarray) -> None:
        with self._lock:
            self._blocks.append(block.copy())
            self._sample_count += block.shape[0]
            while self._sample_count > self._max_samples and len(self._blocks) > 1:
                head = self._blocks.popleft()
                self._sample_count -= head.shape[0]

    def snapshot(self, want_ms: int) -> np.ndarray:
        want_samples = int(self.samplerate * want_ms / 1000)
        with self._lock:
            if not self._blocks:
                return np.zeros((0, self.channels), dtype=np.int16)
            joined = np.concatenate(list(self._blocks), axis=0)
        if joined.shape[0] > want_samples:
            joined = joined[-want_samples:]
        return joined


class CallWriter:
    """Streaming WAV writer that renames on close. Avoids partial-name pickup
    by writing to a `.part` file and renaming atomically when finalised."""

    def __init__(
        self,
        output_dir: Path,
        samplerate: int,
        channels: int,
        glg: GlgState,
        started_at: float,
    ):
        self.output_dir = output_dir
        self.samplerate = samplerate
        self.channels = channels
        self.glg = glg
        self.started_at = started_at
        self.last_audio_at = started_at
        self.frames_written = 0

        ts = time.localtime(started_at)
        date = time.strftime("%Y%m%d", ts)
        clock = time.strftime("%H%M%S", ts)
        syslbl = _safe_label(glg.system_label or "system")
        tglbl = _safe_label(glg.talkgroup_label or "talkgroup")
        hz = glg.frequency_hz if glg.frequency_hz is not None else 0
        # Mask compatible with rdio-scanner DirWatch:
        #   #DATE_#TIME_#SYSLBL_#TGLBL_#HZ.wav
        self.final_name = f"{date}_{clock}_{syslbl}_{tglbl}_{hz}.wav"
        self.part_path = output_dir / (self.final_name + ".part")
        self.final_path = output_dir / self.final_name

        self._wave = wave.open(str(self.part_path), "wb")
        self._wave.setnchannels(channels)
        self._wave.setsampwidth(2)  # int16
        self._wave.setframerate(samplerate)

    def write(self, block: np.ndarray) -> None:
        if block.size == 0:
            return
        if block.dtype != np.int16:
            block = _to_int16(block)
        # wave expects bytes in interleaved frame order.
        self._wave.writeframes(block.tobytes())
        self.frames_written += block.shape[0]
        self.last_audio_at = time.time()

    def finalise(self) -> Optional[Path]:
        try:
            self._wave.close()
        except Exception as exc:
            LOG.warning("wave close failed for %s: %s", self.part_path, exc)
        # Discard absurdly short captures - typically squelch noise blips.
        min_frames = int(self.samplerate * 0.25)
        if self.frames_written < min_frames:
            try:
                self.part_path.unlink(missing_ok=True)
            except Exception:
                pass
            return None
        try:
            os.replace(self.part_path, self.final_path)
        except OSError as exc:
            LOG.error("rename %s -> %s failed: %s", self.part_path, self.final_path, exc)
            return None
        return self.final_path


def _safe_label(s: str) -> str:
    s = s.strip() or "unknown"
    s = _FNAME_SAFE.sub("_", s)
    return s[:48] or "unknown"


def _to_int16(block: np.ndarray) -> np.ndarray:
    if np.issubdtype(block.dtype, np.floating):
        clipped = np.clip(block, -1.0, 1.0)
        return (clipped * 32767.0).astype(np.int16)
    return block.astype(np.int16)


class Recorder:
    def __init__(self, cfg: configparser.SectionProxy):
        self.serial_port = cfg.get("serial_port", "/dev/ttyUSB0")
        self.serial_baud = cfg.getint("serial_baud", 115200)
        self.audio_device = cfg.get("audio_device", "").strip() or None
        self.samplerate = cfg.getint("samplerate", 22050)
        self.channels = cfg.getint("channels", 1)
        self.blocksize = cfg.getint("blocksize", 1024)
        self.pre_roll_ms = cfg.getint("pre_roll_ms", DEFAULT_PRE_ROLL_MS)
        self.min_silence_ms = cfg.getint("min_silence_ms", DEFAULT_MIN_SILENCE_MS)
        self.poll_hz = cfg.getfloat("poll_hz", 5.0)
        self.output_dir = Path(cfg.get("output_dir", "/home/pi/scanner-calls")).expanduser()

        self.output_dir.mkdir(parents=True, exist_ok=True)

        self._ring = AudioRing(self.samplerate, self.channels, self.pre_roll_ms)
        self._stop = threading.Event()
        self._writer: Optional[CallWriter] = None
        self._writer_lock = threading.Lock()
        self._audio_queue: queue.Queue = queue.Queue(maxsize=64)
        self._audio_dev_resolved: Optional[int] = None

    # ---- public ---------------------------------------------------------

    def run(self) -> int:
        self._resolve_audio_device()
        signal.signal(signal.SIGTERM, self._on_signal)
        signal.signal(signal.SIGINT, self._on_signal)

        threads = [
            threading.Thread(target=self._audio_consumer, name="audio-consumer", daemon=True),
            threading.Thread(target=self._serial_loop, name="serial-glg", daemon=True),
        ]
        for t in threads:
            t.start()

        LOG.info(
            "started: serial=%s audio=%s output=%s",
            self.serial_port,
            self.audio_device or "<default>",
            self.output_dir,
        )

        try:
            with sd.InputStream(
                device=self._audio_dev_resolved,
                channels=self.channels,
                samplerate=self.samplerate,
                blocksize=self.blocksize,
                dtype="int16",
                callback=self._audio_callback,
            ):
                while not self._stop.is_set():
                    time.sleep(0.25)
        except Exception as exc:
            LOG.error("audio stream failed: %s", exc)
            return 1
        finally:
            self._close_writer(force=True)
        return 0

    def stop(self) -> None:
        self._stop.set()

    # ---- audio ----------------------------------------------------------

    def _resolve_audio_device(self) -> None:
        if self.audio_device is None:
            self._audio_dev_resolved = None
            return
        if self.audio_device.isdigit():
            self._audio_dev_resolved = int(self.audio_device)
            return
        for idx, dev in enumerate(sd.query_devices()):
            if dev.get("max_input_channels", 0) > 0 and self.audio_device in dev["name"]:
                self._audio_dev_resolved = idx
                return
        raise SystemExit(f"audio_device {self.audio_device!r} not found; use --list-audio to inspect")

    def _audio_callback(self, indata, frames, time_info, status):
        if status:
            LOG.debug("audio status: %s", status)
        try:
            self._audio_queue.put_nowait(indata.copy())
        except queue.Full:
            # Drop oldest to keep up; the ring is best-effort.
            try:
                self._audio_queue.get_nowait()
                self._audio_queue.put_nowait(indata.copy())
            except queue.Empty:
                pass

    def _audio_consumer(self) -> None:
        while not self._stop.is_set():
            try:
                block = self._audio_queue.get(timeout=0.25)
            except queue.Empty:
                continue
            self._ring.push(block)
            with self._writer_lock:
                if self._writer is not None:
                    self._writer.write(block)

    # ---- serial / GLG ---------------------------------------------------

    def _serial_loop(self) -> None:
        period = 1.0 / max(self.poll_hz, 0.1)
        backoff = 1.0
        while not self._stop.is_set():
            try:
                with serial.Serial(self.serial_port, self.serial_baud, timeout=0.3) as ser:
                    backoff = 1.0
                    LOG.info("serial open: %s @ %d", self.serial_port, self.serial_baud)
                    while not self._stop.is_set():
                        glg = self._poll_glg(ser)
                        if glg is not None:
                            self._handle_glg(glg)
                        self._maybe_finalise_idle()
                        time.sleep(period)
            except serial.SerialException as exc:
                LOG.warning("serial error %s; reconnecting in %.1fs", exc, backoff)
                self._sleep_interruptible(backoff)
                backoff = min(backoff * 2, 30.0)

    def _poll_glg(self, ser: serial.Serial) -> Optional[GlgState]:
        try:
            ser.reset_input_buffer()
            ser.write(b"GLG\r")
            line = ser.readline().decode("ascii", errors="replace")
        except serial.SerialException:
            raise
        except Exception as exc:
            LOG.debug("GLG read: %s", exc)
            return None
        if not line:
            return None
        LOG.debug("GLG <- %s", line.strip())
        return GlgState.parse(line)

    def _sleep_interruptible(self, seconds: float) -> None:
        end = time.time() + seconds
        while not self._stop.is_set() and time.time() < end:
            time.sleep(0.1)

    # ---- call lifecycle -------------------------------------------------

    def _handle_glg(self, glg: GlgState) -> None:
        with self._writer_lock:
            if glg.squelch_open:
                if self._writer is None:
                    self._open_writer(glg)
                elif self._writer.glg.call_key() != glg.call_key():
                    # TGID/freq changed mid-stream - close current, start new.
                    self._close_writer_locked()
                    self._open_writer(glg)
                else:
                    self._writer.last_audio_at = time.time()
            # squelch closed: don't tear down immediately; idle check handles it.

    def _maybe_finalise_idle(self) -> None:
        with self._writer_lock:
            if self._writer is None:
                return
            silence_for_ms = (time.time() - self._writer.last_audio_at) * 1000.0
            if silence_for_ms >= self.min_silence_ms:
                self._close_writer_locked()

    def _open_writer(self, glg: GlgState) -> None:
        started = time.time()
        try:
            writer = CallWriter(self.output_dir, self.samplerate, self.channels, glg, started)
        except Exception as exc:
            LOG.error("could not open call wav: %s", exc)
            return
        # Prepend pre-roll.
        preroll = self._ring.snapshot(self.pre_roll_ms)
        if preroll.shape[0]:
            writer.write(preroll)
        self._writer = writer
        LOG.info(
            "call open: sys=%s tg=%s freq=%s -> %s",
            glg.system_label,
            glg.talkgroup_label,
            glg.frequency_hz,
            writer.final_name,
        )

    def _close_writer(self, force: bool = False) -> None:
        with self._writer_lock:
            self._close_writer_locked(force=force)

    def _close_writer_locked(self, force: bool = False) -> None:
        if self._writer is None:
            return
        writer = self._writer
        self._writer = None
        path = writer.finalise()
        if path is not None:
            LOG.info("call close: %s (%d frames)", path.name, writer.frames_written)
        elif force:
            LOG.info("call discarded (too short)")

    # ---- signals --------------------------------------------------------

    def _on_signal(self, signum, frame) -> None:
        LOG.info("signal %d received, shutting down", signum)
        self.stop()


def _load_config(path: Optional[str]) -> configparser.SectionProxy:
    parser = configparser.ConfigParser()
    candidates = [path] if path else list(DEFAULT_CONFIG_PATHS)
    chosen = None
    for cand in candidates:
        if cand and Path(cand).exists():
            chosen = cand
            break
    if chosen is None:
        LOG.warning("no config file found in %s; using built-in defaults", candidates)
        parser.read_dict({"uniden": {}})
    else:
        parser.read(chosen)
        if "uniden" not in parser:
            raise SystemExit(f"config {chosen!r} missing [uniden] section")
    return parser["uniden"]


def _list_audio() -> int:
    for idx, dev in enumerate(sd.query_devices()):
        marker = "in" if dev.get("max_input_channels", 0) > 0 else "  "
        print(f"  [{idx:>2}] {marker}  {dev['name']}")
    return 0


def _list_serial() -> int:
    try:
        from serial.tools import list_ports
    except ImportError:
        print("pyserial list_ports not available", file=sys.stderr)
        return 1
    for p in list_ports.comports():
        print(f"  {p.device}  {p.description}")
    return 0


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="Uniden BCD996XT companion recorder for rdio-scanner")
    ap.add_argument("-c", "--config", help="path to .ini")
    ap.add_argument("-v", "--verbose", action="store_true", help="debug logging incl. raw GLG")
    ap.add_argument("--list-audio", action="store_true", help="list audio devices and exit")
    ap.add_argument("--list-serial", action="store_true", help="list serial ports and exit")
    args = ap.parse_args(argv)

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        stream=sys.stderr,
    )

    if args.list_audio:
        return _list_audio()
    if args.list_serial:
        return _list_serial()

    cfg = _load_config(args.config)
    rec = Recorder(cfg)
    return rec.run()


if __name__ == "__main__":
    sys.exit(main())
