/*
 * *****************************************************************************
 * Copyright (C) 2019-2021 Chrystian Huot
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

const defaults = {
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
    systems: [{
        id: 11,
        label: 'RSP25MTL1',
        talkgroups: [
            {
                id: 54241,
                label: 'TDB A1',
                name: 'MRC TDB Fire Alpha 1',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 54242,
                label: 'TDB B1',
                name: 'MRC TDB Fire Bravo 1',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54243,
                label: 'TDB B2',
                name: 'MRC TDB Fire Bravo 2',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54248,
                label: 'TDB B3',
                name: 'MRC TDB Fire Bravo 3',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54251,
                label: 'TDB B4',
                name: 'MRC TDB Fire Bravo 4',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54261,
                label: 'TDB B5',
                name: 'MRC TDB Fire Bravo 5',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54244,
                label: 'TDB B6',
                name: 'MRC TDB Fire Bravo 6',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54129,
                label: 'TDB B7',
                name: 'MRC TDB Fire Bravo 7',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54125,
                label: 'TDB B8',
                name: 'MRC TDB Fire Bravo 8',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
        ],
        units: [
            {
                id: 4424001,
                label: 'CAUCA',
            },
        ],
    },
    {
        id: 21,
        label: 'SERAM',
        talkgroups: [
            {
                id: 60040,
                label: 'GENERAL',
                name: 'SERAM General',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 60041,
                label: 'REPART',
                name: 'SERAM Repartition',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50001,
                label: 'SG 1',
                name: 'SERAM Regroupement 1',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50002,
                label: 'SG 2',
                name: 'SERAM Regroupement 2',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50003,
                label: 'SG 3',
                name: 'SERAM Regroupement 3',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50004,
                label: 'SG 4',
                name: 'SERAM Regroupement 4',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50005,
                label: 'SG 5',
                name: 'SERAM Regroupement 5',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50006,
                label: 'SG 6',
                name: 'SERAM Regroupement 6',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50007,
                label: 'SG 7',
                name: 'SERAM Regroupement 7',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50008,
                label: 'SG 8',
                name: 'SERAM Regroupement 8',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 60051,
                label: 'CMD 3',
                name: 'SERAM Commandement 3',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60052,
                label: 'CMD 4',
                name: 'SERAM Commandement 4',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60053,
                label: 'CMD 5',
                name: 'SERAM Commandement 5',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60054,
                label: 'CMD 6',
                name: 'SERAM Commandement 6',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60055,
                label: 'CMD 7',
                name: 'SERAM Commandement 7',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60056,
                label: 'CMD 8',
                name: 'SERAM Commandement 8',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60059,
                label: 'CMD 11',
                name: 'SERAM Commandement 11',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60091,
                label: 'CMD 12',
                name: 'SERAM Commandement 12',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60092,
                label: 'CMD 13',
                name: 'SERAM Commandement 13',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60057,
                label: 'CMD 14',
                name: 'SERAM Commandement 14',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60061,
                label: 'OPS 3',
                name: 'SERAM Operations 3',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60062,
                label: 'OPS 4',
                name: 'SERAM Operations 4',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60063,
                label: 'OPS 5',
                name: 'SERAM Operations 5',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60064,
                label: 'OPS 6',
                name: 'SERAM Operations 6',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60065,
                label: 'OPS 7',
                name: 'SERAM Operations 7',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60066,
                label: 'OPS 8',
                name: 'SERAM Operations 8',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60069,
                label: 'OPS 11',
                name: 'SERAM Operations 11',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60093,
                label: 'OPS 12',
                name: 'SERAM Operations 12',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60094,
                label: 'OPS 13',
                name: 'SERAM Operations 13',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60067,
                label: 'OPS 14',
                name: 'SERAM Operations 14',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
        ],
        units: [{
            id: 702099,
            label: 'DISPATCH',
        }],
    }],
};

module.exports = defaults;
