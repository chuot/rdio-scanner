// migrates pre-7.x exported config files to the current key layout.
// the apiKeys mapping is _id to id.

export function migrateLegacyConfig(raw) {
  const config = structuredClone(raw);

  if (Array.isArray(config.access)) {
    for (const access of config.access) access.id = access._id;
  }

  config.apikeys = config.apiKeys;
  if (Array.isArray(config.apikeys)) {
    for (const apikey of config.apikeys) apikey.id = apikey._id;
  }

  config.dirwatch = config.dirWatch;
  if (Array.isArray(config.dirwatch)) {
    for (const dirwatch of config.dirwatch) dirwatch.id = dirwatch._id;
  }

  if (Array.isArray(config.downstreams)) {
    for (const downstream of config.downstreams) {
      downstream.id = downstream._id;
      downstream.apikey = downstream.apiKey;
    }
  }

  if (Array.isArray(config.groups)) {
    for (const group of config.groups) group.id = group._id;
    config.groups.sort((a, b) => a.label.localeCompare(b.label));
  }

  if (Array.isArray(config.tags)) {
    for (const tag of config.tags) tag.id = tag._id;
    config.tags.sort((a, b) => a.label.localeCompare(b.label));
  }

  if (Array.isArray(config.systems)) {
    for (const system of config.systems) {
      system.systemRef = system.id;
      system.id = system._id;

      if (Array.isArray(system.talkgroups)) {
        for (const talkgroup of system.talkgroups) {
          if (typeof talkgroup.groupId === 'number') talkgroup.groupIds = [talkgroup.groupId];

          talkgroup.talkgroupRef = talkgroup.id;
          delete talkgroup.id;
        }
      }
    }
  }

  return config;
}
