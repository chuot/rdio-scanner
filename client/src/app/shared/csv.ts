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

// RFC 4180-ish CSV parser. Honors:
//   - quoted fields containing commas, CR or LF
//   - "" escape inside quoted fields
//   - both LF and CRLF line endings
// Empty lines are dropped. The previous regex-based splitter would corrupt
// any field that contained a comma even when properly quoted.
export function parseCsv(input: string): string[][] {
    // Strip UTF-8 BOM. Spreadsheets (Excel, Numbers, Notepad on Windows) write
    // CSVs that start with U+FEFF, which would otherwise end up as the first
    // character of column 0 and quietly break any "starts with a digit"
    // validation downstream.
    if (input.charCodeAt(0) === 0xFEFF) {
        input = input.slice(1);
    }

    const rows: string[][] = [];
    let row: string[] = [];
    let field = '';
    let inQuotes = false;

    const pushField = () => {
        row.push(field);
        field = '';
    };

    const pushRow = () => {
        if (row.length > 1 || row[0] !== '') {
            rows.push(row);
        }
        row = [];
    };

    for (let i = 0; i < input.length; i++) {
        const ch = input[i];

        if (inQuotes) {
            if (ch === '"') {
                if (input[i + 1] === '"') {
                    field += '"';
                    i++;
                } else {
                    inQuotes = false;
                }
            } else {
                field += ch;
            }
            continue;
        }

        if (ch === '"') {
            inQuotes = true;
        } else if (ch === ',') {
            pushField();
        } else if (ch === '\n' || ch === '\r') {
            pushField();
            if (ch === '\r' && input[i + 1] === '\n') i++;
            pushRow();
        } else {
            field += ch;
        }
    }

    if (field.length > 0 || row.length > 0) {
        pushField();
        pushRow();
    }

    return rows;
}
