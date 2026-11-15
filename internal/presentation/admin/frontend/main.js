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
 * Admin entry point: asks the operator about a service-worker update, boots the admin context,
 * and mounts the rdio-admin shell (login screen or sidebar navigation, decided by the auth store
 * slice). The shell element is in index.html, which is what lets this page be served from /admin/
 * with nothing but its own bundle.
 */

import '../../common/frontend/styles/theme.css';
import './styles/admin.css';

import './presentation/admin/rdio-admin.js';

import { promptForUpdate } from '../../common/frontend/presentation/update-prompt.js';
import { getAdminContext } from './application/admin-context.js';

// Asked before the sections are built: an operator decides about a new version before the page is
// half-built around them, and a waiting worker is reached whatever the answer.
promptForUpdate('A new version of the administration interface is available. Would you like to reload the page?');

getAdminContext().useCases.boot();
