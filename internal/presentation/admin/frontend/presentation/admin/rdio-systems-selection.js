// rdio-systems-selection: tri-state Everything/Groups/Tags/Systems/Talkgroups dialog
// every check cascades through the domain model and the tree re-renders result: '*' or [{
// id: systemRef, talkgroups: '*' | [talkgroupRef...] }], null on cancel.

import { openDialog } from '../../../../common/frontend/presentation/ui/primitives.js';
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

// rowLabel names a row by its label with (ref) in parentheses after it groups
// and tags have no reference number, so their rows carry the label alone an
// empty row falls back to the unnamed text instead of undefined or null.
function rowLabel({ label, ref, unnamed }) {
  const name = typeof label === 'string' ? label.trim() : '';

  const number = typeof ref === 'number' && Number.isFinite(ref) && ref >= 1 ? ref : null;

  if (name) return number === null ? name : `${name} (${number})`;

  return number === null ? unnamed : String(number);
}

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

    // the tree repaints on every activation, which throws away the focused control
    // the activated row's dataset.node is recorded before the rebuild the keyboard
    // is put back on that same row after it.
    const activated = document.activeElement?.dataset?.node;

    const root = document.createElement('div');
    root.className = 'rdio-systems-selection';

    root.append(
      this.checkbox({
        label: 'Everything',
        className: 'everything',
        node: 'everything',
        checked: model.all,
        indeterminate: model.indeterminate.everything,
        onChange: (checked) => {
          setEverything(model, checked);
          this.render();
        },
      }),
    );

    root.append(this.listBlock('Groups', model.groups.map((group, index) => ({
      label: rowLabel({ label: group.label, unnamed: 'New Group' }),
      node: `group:${group.id}`,
      checked: group.checked,
      indeterminate: model.indeterminate.groups[index],
      onChange: (checked) => {
        setGroup(model, index, checked);
        this.render();
      },
    }))));

    root.append(this.listBlock('Tags', model.tags.map((tag, index) => ({
      label: rowLabel({ label: tag.label, unnamed: 'New Tag' }),
      node: `tag:${tag.id}`,
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
          label: rowLabel({ label: system.label, ref: system.id, unnamed: 'New system' }),
          className: 'system',
          node: `system:${system.id}`,
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
            label: rowLabel({ label: talkgroup.label, ref: talkgroup.id, unnamed: 'New talkgroup' }),
            node: `talkgroup:${talkgroup.id}`,
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

    if (activated) root.querySelector(`[data-node="${activated}"]`)?.focus();
  }

  checkbox({ label, className = '', node, checked, indeterminate = false, onChange }) {
    const wrapper = document.createElement('label');
    wrapper.className = `rdio-tree-item ${className}`.trim();

    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = !!checked;
    input.indeterminate = !!indeterminate;
    input.addEventListener('change', () => onChange(input.checked));

    // data-node carries the row's identity: the rebuild hands focus back by it
    if (node) input.setAttribute('data-node', node);

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
