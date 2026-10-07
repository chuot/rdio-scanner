// rdio-systems-selection (T036): tri-state selection dialog
// (legacy config/systems-selection). The tree Everything → Groups / Tags /
// Systems → Talkgroups is rendered from the pure domain model; every check
// cascades through it and the tree re-renders. Result: '*' or
// [{ id: systemRef, talkgroups: '*' | [talkgroupRef...] }], or null on cancel.

import { openDialog } from '../ui/primitives.js';
import {
  createSelectionModel,
  emitSelection,
  hydrateSelection,
  setEverything,
  setGroup,
  setSystem,
  setTag,
  setTalkgroup,
} from '../../domain/systems-selection.js';

class RdioSystemsSelection extends HTMLElement {
  setup(config, value) {
    this.model = createSelectionModel(config);

    hydrateSelection(this.model, value);

    this.render();
  }

  disconnectedCallback() {
    // Escape / backdrop close without an explicit action counts as cancel.
    this.onDismiss?.();
  }

  render() {
    const model = this.model;

    const root = document.createElement('div');
    root.className = 'rdio-systems-selection';

    root.append(
      this.checkbox({
        label: 'Everything',
        className: 'everything',
        checked: model.all,
        indeterminate: model.indeterminate.everything,
        onChange: (checked) => {
          setEverything(model, checked);
          this.render();
        },
      }),
    );

    root.append(this.listBlock('Groups', model.groups.map((group, index) => ({
      label: group.label ?? String(group.id),
      checked: group.checked,
      indeterminate: model.indeterminate.groups[index],
      onChange: (checked) => {
        setGroup(model, index, checked);
        this.render();
      },
    }))));

    root.append(this.listBlock('Tags', model.tags.map((tag, index) => ({
      label: tag.label ?? String(tag.id),
      checked: tag.checked,
      indeterminate: model.indeterminate.tags[index],
      onChange: (checked) => {
        setTag(model, index, checked);
        this.render();
      },
    }))));

    const systemsBlock = document.createElement('fieldset');
    systemsBlock.className = 'tree-systems';

    const systemsLegend = document.createElement('legend');
    systemsLegend.textContent = 'Systems';
    systemsBlock.append(systemsLegend);

    model.systems.forEach((system, systemIndex) => {
      systemsBlock.append(
        this.checkbox({
          label: system.label ?? String(system.id),
          className: 'system',
          checked: system.all,
          indeterminate: model.indeterminate.systems[systemIndex],
          onChange: (checked) => {
            setSystem(model, systemIndex, checked);
            this.render();
          },
        }),
      );

      const talkgroups = document.createElement('div');
      talkgroups.className = 'indent';

      system.talkgroups.forEach((talkgroup, talkgroupIndex) => {
        talkgroups.append(
          this.checkbox({
            label: `${talkgroup.id} ${talkgroup.label ?? ''}`.trim(),
            checked: talkgroup.checked,
            onChange: (checked) => {
              setTalkgroup(model, systemIndex, talkgroupIndex, checked);
              this.render();
            },
          }),
        );
      });

      systemsBlock.append(talkgroups);
    });

    root.append(systemsBlock);

    this.replaceChildren(root);
  }

  checkbox({ label, className = '', checked, indeterminate = false, onChange }) {
    const wrapper = document.createElement('label');
    wrapper.className = `rdio-tree-item ${className}`.trim();

    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = !!checked;
    input.indeterminate = !!indeterminate;
    input.addEventListener('change', () => onChange(input.checked));

    const text = document.createElement('span');
    text.textContent = label;

    wrapper.append(input, text);

    return wrapper;
  }

  listBlock(title, items) {
    const block = document.createElement('fieldset');
    block.className = `tree-${title.toLowerCase()}`;

    const legend = document.createElement('legend');
    legend.textContent = title;
    block.append(legend);

    for (const item of items) block.append(this.checkbox(item));

    return block;
  }
}

customElements.define('rdio-systems-selection', RdioSystemsSelection);

export function openSystemsSelectionDialog({ config, value }) {
  return new Promise((resolve) => {
    let settled = false;

    const settle = (result) => {
      if (settled) return;

      settled = true;

      resolve(result);
    };

    const tree = document.createElement('rdio-systems-selection');

    tree.onDismiss = () => settle(null);

    tree.setup(config, value);

    openDialog({
      title: 'Systems and talkgroups',
      content: tree,
      actions: [
        { label: 'Cancel', onClick: () => settle(null) },
        { label: 'Select', onClick: () => settle(emitSelection(tree.model)) },
      ],
    });
  });
}
