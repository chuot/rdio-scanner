import { CommonModule } from '@angular/common';
import { ModuleWithProviders, NgModule } from '@angular/core';
import { FlexLayoutModule } from '@angular/flex-layout';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { AppApolloModule } from './apollo/apollo.module';
import { AppLocaleModule } from './locale/locale.module';
import { AppMaterialModule } from './material/material.module';

@NgModule({
    exports: [
        AppApolloModule,
        AppLocaleModule,
        AppMaterialModule,
        CommonModule,
        FlexLayoutModule,
        FormsModule,
        ReactiveFormsModule,
    ],
})
export class AppSharedModule {
    static forRoot(): ModuleWithProviders {
        return {
            ngModule: AppSharedModule,
            providers: [
                ...AppApolloModule.forRoot().providers,
                ...AppLocaleModule.forRoot().providers,
            ],
        };
    }
}
