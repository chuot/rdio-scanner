// Alert and LED color priority resolution
// (legacy rdio-scanner.service.ts playAlert, main.component.ts getLedColor):
// group → tag → system → talkgroup.

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

// The tone the winning level names, looked up in the tone table the server sends. A name the table
// does not carry, or a table entry that is not a sequence, is no tone at all.
export function resolveAlertSequence(call, config) {
  if (!call || !config) return undefined;

  const name = resolveAlert(call, config);

  if (!name) return undefined;

  const sequence = config?.alerts?.[name];

  return Array.isArray(sequence) && sequence.length > 0 ? sequence : undefined;
}

export const LED_COLORS = ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];

export function resolveLedColor(call, colors = LED_COLORS) {
  const group = (call?.groupsData ?? []).find((g) => g.led);

  const color =
    group?.led ||
    call?.tagData?.led ||
    call?.systemData?.led ||
    call?.talkgroupData?.led;

  return color && colors.includes(color) ? color : 'green';
}
