import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { AppMaterialModule } from './material/material.module';

@NgModule({
    exports: [
        AppMaterialModule,
        CommonModule,
        FormsModule,
        ReactiveFormsModule,
    ],
})
export class AppSharedModule { }
