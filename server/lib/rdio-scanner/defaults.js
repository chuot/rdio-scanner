/*
 * *****************************************************************************
 * Copyright (C) 2019-2021 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

'use strict';

export const defaults = {
    adminPassword: 'rdio-scanner',
    dirWatch: {
        delay: 0,
        deleteAfter: false,
        usePolling: false,
    },
    keypadBeeps: {
        uniden: {
            activate: [
                {
                    begin: 0,
                    end: 0.05,
                    frequency: 1200,
                    type: 'square',
                },
            ],
            deactivate: [
                {
                    begin: 0,
                    end: 0.1,
                    frequency: 1200,
                    type: 'square',
                },
                {
                    begin: 0.1,
                    end: 0.2,
                    frequency: 925,
                    type: 'square',
                },
            ],
            denied: [
                {
                    begin: 0,
                    end: 0.05,
                    frequency: 925,
                    type: 'square',
                },
                {
                    begin: 0.1,
                    end: 0.15,
                    frequency: 925,
                    type: 'square',
                },
            ],
        },
        whistler: {
            activate: [
                {
                    begin: 0,
                    end: 0.05,
                    frequency: 2000,
                    type: 'triangle',
                }
            ],
            deactivate: [
                {
                    begin: 0,
                    end: 0.04,
                    frequency: 1500,
                    type: 'triangle',
                },
                {
                    begin: 0.04,
                    end: 0.08,
                    frequency: 1400,
                    type: 'triangle',
                },
            ],
            denied: [
                {
                    begin: 0,
                    end: 0.04,
                    frequency: 1400,
                    type: 'triangle',
                },
                {
                    begin: 0.05,
                    end: 0.09,
                    frequency: 1400,
                    type: 'triangle',
                },
                {
                    begin: 0.1,
                    end: 0.14,
                    frequency: 1400,
                    type: 'triangle'
                },
            ],
        },
    },
    options: {
        autoPopulate: true,
        dimmerDelay: 5000,
        disableAudioConversion: false,
        disableDuplicateDetection: false,
        duplicateDetectionTimeFrame: 500,
        keypadBeeps: 'uniden',
        pruneDays: 7,
        sortTalkgroups: false,
    },
    systems: [],
};
