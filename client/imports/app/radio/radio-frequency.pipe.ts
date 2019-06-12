import { Pipe, PipeTransform } from '@angular/core';

@Pipe({
    name: 'freq',
})
export class AppRadioFrequencyPipe implements PipeTransform {
    formated: string;

    value: number;

    constructor() { }

    transform(value: number): string {
        if (typeof value !== 'number') {
            return value;

        } else if (value === this.value) {
            return this.formated;

        } else {
            this.value = value;

            this.formated = (typeof value === 'number' ? value : 0)
                .toString()
                .padStart(9, '0')
                .replace(/(\d)(?=(\d{3})+$)/g, '$1 ')
                .concat(' Hz');

            return this.formated;
        }
    }
}
