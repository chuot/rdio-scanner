/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 * ****************************************************************************
 */

/*
 * Scanner entry point: registers the listener elements, boots the listener context, and asks
 * the operator about a service-worker update through the prompt the two applications share.
 */

import '../../common/frontend/styles/theme.css';
import './styles/rdio.css';

import './presentation/listener/rdio-app.js';
import './presentation/listener/rdio-controls.js';
import './presentation/listener/rdio-display.js';
import './presentation/listener/rdio-search-panel.js';
import './presentation/listener/rdio-select-panel.js';
import './presentation/listener/rdio-status.js';

import { promptForUpdate } from '../../common/frontend/presentation/update-prompt.js';
import { getAppContext } from './application/context.js';

// A waiting service worker says a new build is waiting; the operator is asked before anything
// reloads, and only a yes hands that worker its skip.
promptForUpdate('A new version of the scanner webapp is available. Would you like to reload the page?');

getAppContext();
