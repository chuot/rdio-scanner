# Rdio Scanner Web Application

## The Main Screen

![Main Screen](./images/rdio_scanner_main.png?raw=true "Main Screen")

### LED Area

![LED Area](./images/rdio_scanner_led.png?raw=true "LED Area")

The LED is illuminated when there is active audio and it blinks if audio is paused.

The color is _green_ by default, but can be customized in `config.json` by the system or by the talkgroup.

### Display Area

![Display Area](./images/rdio_scanner_display.png?raw=true "Display Area")

- First row
  - **14:29** - Current time
  - **Q: 27** - Number of audio files in the listening queue
- Second row
  - **RSP25MTL1** - System label
  - **Security** - Talkgroup tag
- Third row
  - **MSURGEN** - Talkgroup label
  - **14:26** - The audio file recorded time
- Fourth row
  - **YMX Security Dispatch** - Talkgroup full name
- Fifth row
  - **F: 774 031 250 Hz** - Call frequency on which the audio file was recorded. The name of the audio file will be displayed instead.
  - **TGID: 56204** - Talkgroup ID
- Sixth row
  - **E: 0** - Recorder's decoding errors
  - **S: 0** - Recorder's spike errors
  - **UID: 4612205** - the unit ID or alias name
- History - The last five played audio files

> Note that you can double-click, double-tap the display area, press the `f key` or the `tab key` to switch to full-screen display (also on mobile devices).

### Control area

![Control Area](./images/rdio_scanner_control.png?raw=true "Control Area")

| Button                                                                                | Keyboard | Description                                                                                                                                                                                                                                                                                                                                              |
| ------------------------------------------------------------------------------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ![LIVE FEED](./images/rdio_scanner_control_livefeed_offline.png?raw=true "LIVE FEED") | l        | When active, incoming audio will be played according to the active systems/talkgroups on the **SELECT TG** panel. The LED can also be _yellow_ if playing audio from the archive while **LIVE FEED** is inactive, this is called **offline playback mode**. Disabling **LIVE FEED** will also stop any playing audio and will clear the listening queue. |
| ![HOLD SYS](./images/rdio_scanner_control_holdsys.png?raw=true "HOLD SYS")            | s        | Temporarily maintain the current system in live feed mode.                                                                                                                                                                                                                                                                                               |
| ![HOLD TG](./images/rdio_scanner_control_holdtg.png?raw=true "HOLD TG")               | t        | Temporarily maintain the current talkgroup in live feed mode.                                                                                                                                                                                                                                                                                            |
| ![REPLAY LAST](./images/rdio_scanner_control_replay.png?raw=true "REPLAY LAST")       | r        | Replay the current audio from the beginning or the previous one if there is none active.                                                                                                                                                                                                                                                                 |
| ![SKIP NEXT](./images/rdio_scanner_control_skip.png?raw=true "SKIP NEXT")             | n        | Stop the audio currently playing and play the next one in the listening queue. This is useful when playing boring or encrypted sound.                                                                                                                                                                                                                    |
| ![AVOID](./images/rdio_scanner_control_avoid.png?raw=true "AVOID")                    | a        | Activate and deactivate the talkgroup from the current or previous audio.                                                                                                                                                                                                                                                                                |
| ![SEARCH CALL](./images/rdio_scanner_control_search.png?raw=true "SEARCH CALL")       | &larr;   | Display the archived audio panel.                                                                                                                                                                                                                                                                                                                        |
| ![PAUSE](./images/rdio_scanner_control_pause.png?raw=true "PAUSE")                    | p, space | Stop playing queue audio. Useful if you have to answer the phone without losing what's queued up for playing.                                                                                                                                                                                                                                            |
| ![SELECT TG](./images/rdio_scanner_control_select.png?raw=true "SELECT TG")           | &rarr;   | Display the systems/talkgroups selection panel where you decide which audio you want to listen to in \*_LIVE FEED_ (not in offline playback mode).                                                                                                                                                                                                       |

### Systems/Talkgroups selection panel

![Systems/Talkgroups Selection](./images/rdio_scanner_select.png?raw=true "Systems/Talkgroups Selection")

Here you select which systems/talkgroups/groups you want to listen to while in **LIVE FEED** mode.

> The selection panel has no effect if you play calls from the archived audio panel.

#### Group selection section

![Groups Selection](./images/rdio_scanner_select_group.png?raw=true "Groups Selection")

The first section concerns group selection. This section can be disabled from the configuration file, but is active by default. Each button has three states:

| Button                                                                        | State                                                                                                                                                                              |
| ----------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ![ON](./images/rdio_scanner_select_group_on.png?raw=true "ON")                | All talkgroups, from any systems, that correspond to this group are actives. If you press it while it is active, it will make these talkgroups inactives.                          |
| ![OFF](./images/rdio_scanner_select_group_off.png?raw=true "OFF")             | All talkgroups, from any systems, that correspond to this group are inactives. If you press it while it is inactive, it will make these talkgroups actives.                        |
| ![PARTIAL](./images/rdio_scanner_select_group_partial.png?raw=true "PARTIAL") | Some talkgroups, from any systems, that correspond to this group are actives and some are inactives. If you press it while it is active, it will make all these talkgroups active. |
| ![ALL OFF](./images/rdio_scanner_select_alloff.png?raw=true "PARTIAL")        | Make every groups inactives, thereby disabling all talkgroups.                                                                                                                     |
| ![ALL ON](./images/rdio_scanner_select_allon.png?raw=true "PARTIAL")          | Make every groups active, thereby enaabling all talkgroups.                                                                                                                        |

#### Systems/Talkgroups selection section

![Systems Selection](./images/rdio_scanner_select_system.png?raw=true "Systems Selection")

This is much like the group selection section, but for each system. There is also just states for each button:

| Button                                                                 | State                                                                                                                |
| ---------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| ![ON](./images/rdio_scanner_select_system_on.png?raw=true "ON")        | The talkgroup from this system is active. If you press it while it is active, it will make the talkgroup inactive.   |
| ![OFF](./images/rdio_scanner_select_system_off.png?raw=true "OFF")     | The talkgroup from this system is inactive. If you press it while it is inactive, it will make the talkgroup active. |
| ![ALL OFF](./images/rdio_scanner_select_alloff.png?raw=true "PARTIAL") | Make inactive every talkgroups from this system.                                                                     |
| ![ALL ON](./images/rdio_scanner_select_allon.png?raw=true "PARTIAL")   | Make active every talkgroups from this system.                                                                       |

### Archived audio panel

![Call Search](./images/rdio_scanner_search.png?raw=true "Call Search")

#### Archived audio list

![Search List](./images/rdio_scanner_search_list.png?raw=true "Search List")

This section presents the list of archived audio files. Depending on the **LIVE FEED** mode you are at, the replay function behaves differently:

| Mode                                                                                                  | Function                                                                                                                                                                                                                                                                                                                   |
| ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ![LIVE FEED ON](./images/rdio_scanner_control_livefeed.png?raw=true "LIVE FEED ON")                   | while **LIVE FEED** is active, press the **PLAY** button to play this audio file. If another audio file is playing, it will be stopped and the one you just selected will be played instead. After playback finishes, the listening queue will resume as normal.                                                           |
| ![LIVE FEED PARTIAL](./images/rdio_scanner_control_livefeed_offline.png?raw=true "LIVE FEED PARTIAL") | This is the **offline playback mode** which can be activated only if **LIVE FEED** is inactive. Press the **PLAY** button to play this audio file. After playback is finishes, the next audio file from in list is played. While the audio is playing, pressing the **STOP** button will cancel the offline playback mode. |

At the bottom right of this section is the _paginator_ to browse the whole database. This _paginator_ is disabled while in offline playback mode.

> Note that offline playback mode always requires your web application to be online. It is called like that because it plays archived audio files.

#### Archived audio filter

![Search Filter](./images/rdio_scanner_search_filter.png?raw=true "Search Filter")

This is the section where you filter archive audio to a specific date, system and talkgroup. You can also change the list order.

There is also a small slider that changes the **PLAY** buttons to **DOWNLOAD** buttons. This allows you to download audio files individually regardless of the playback mode you use.

> Note that if you change any filter while in offline playback mode, it will deactivate it.

### Finally

Happy Rdio scanning !
