/**
 * @license
 * Copyright Saubeo Solutions All Rights Reserved
 *
 * This source code is proprietary and confidential
 * Unauthorized copying of this file, via any medium is strictly prohibited
 */

import { Routes } from '@angular/router';
import { routes as rdioScannerRoutes } from './pages/rdio-scanner';

export const routes: Routes = [
    ...rdioScannerRoutes,
    {
        path: '**',
        pathMatch: 'full',
        redirectTo: '',
    },
];
