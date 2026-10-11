// Admin panel todo list derivation (legacy todos.component.ts rebuildTodos).

export function computeTodos(config, { passwordNeedChange } = {}) {
  const todos = [];

  if (passwordNeedChange) {
    todos.push({
      level: 'warn',
      message: 'You are using the default admin password, please change it from the tools / admin password menu.',
    });
  }

  if (!config?.systems?.length) {
    todos.push({
      level: 'info',
      message: 'No systems defined. Define one from the Systems menu, or turn on Options ▸ Auto Populate to have them created for you.',
      // The todo names the control that clears it, so the operator can follow the
      // path the sentence states instead of hunting for it.
      step: { section: 'options', control: 'autoPopulate' },
    });
  }

  if (!config?.apikeys?.length && !config?.dirwatch?.length) {
    todos.push({
      level: 'info',
      message: 'No apikeys or dirwatch defined. Set at least one to allow ingesting audio files.',
    });
  }

  return todos;
}
