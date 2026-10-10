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
      message: 'No systems defined. You can define one from the systems menu, or turn on Auto Populate in the Options menu to have them created for you.',
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
