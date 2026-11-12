// Alert priority resolution: group, then tag, then system, then talkgroup.
//
// LED colour resolution reads group, then tag, then talkgroup. The server folds the system
// colour into the talkgroup entry of the configuration it sends, so that entry always names
// a colour and the colour chain has no system level to read.

export function resolveAlert(call, config) {
  for (const groupData of call?.groupsData ?? []) {
    const group = config.groupsData?.find((g) => g.label === groupData.label) ?? groupData;
    if (group?.alert) return group.alert;
  }

  const tag = config.tagsData?.find((t) => t.label === call?.tagData?.label);
  if (tag?.alert) return tag.alert;

  if (call?.systemData?.alert) return call.systemData.alert;

  if (call?.talkgroupData?.alert) return call.talkgroupData.alert;

  return undefined;
}

// the tone the winning level names, from the server's tone table; a
// missing name or non-sequence entry is no tone.
export function resolveAlertSequence(call, config) {
  if (!call || !config) return undefined;

  const name = resolveAlert(call, config);

  if (!name) return undefined;

  const sequence = config?.alerts?.[name];

  return Array.isArray(sequence) && sequence.length > 0 ? sequence : undefined;
}

export const LED_COLORS = ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];

// the colour to light an LED with, from the levels the configuration leaves on the
// call; the server has already resolved its own default into the talkgroup entry, so a
// name outside the eight is no colour and the answer is green.
export function resolveLedColor(call, colors = LED_COLORS) {
  const group = (call?.groupsData ?? []).find((g) => g.led);

  const color = group?.led || call?.tagData?.led || call?.talkgroupData?.led;

  return color && colors.includes(color) ? color : 'green';
}
