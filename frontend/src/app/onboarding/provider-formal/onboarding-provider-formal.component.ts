import { Component, ViewEncapsulation } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule, Validators } from '@angular/forms';
import { latinOnly } from '../validators/latin-only.validator';
import { FormalLandingComponent } from './components/landing/formal-landing.component';
import { LegalRepresentativeComponent } from './components/legal-representative/legal-representative.component';
import { LearComponent } from './components/lear/lear.component';
import { ContractualDocumentsComponent } from './components/contractual-documents/contractual-documents.component';
import { FormalThankYouComponent } from './components/thank-you/formal-thank-you.component';

@Component({
  selector: 'app-onboarding-provider-formal',
  standalone: true,
  imports: [
    ReactiveFormsModule,
    FormalLandingComponent,
    LegalRepresentativeComponent,
    LearComponent,
    ContractualDocumentsComponent,
    FormalThankYouComponent,
  ],
  templateUrl: './onboarding-provider-formal.component.html',
  styleUrl: './onboarding-provider-formal.component.css',
  encapsulation: ViewEncapsulation.None
})
export class OnboardingProviderFormalComponent {
  currentStep = 0;

  formalForm = new FormGroup({
    legalRepresentative: new FormGroup({
      firstName: new FormControl('', [Validators.required, latinOnly]),
      lastName: new FormControl('', [Validators.required, latinOnly]),
      email: new FormControl('', [Validators.required, Validators.email]),
      nationality: new FormControl(''),
      idCardNumber: new FormControl('', [latinOnly]),
    }),
    lear: new FormGroup({
      firstName: new FormControl('', [Validators.required, latinOnly]),
      lastName: new FormControl('', [Validators.required, latinOnly]),
      email: new FormControl('', [Validators.required, Validators.email]),
      nationality: new FormControl(''),
      professionalAddress: new FormControl('', [latinOnly]),
      idCardNumber: new FormControl('', [latinOnly]),
      mobilePhone: new FormControl('', [latinOnly]),
      isAuthorised: new FormControl(false, [Validators.requiredTrue]),
    }),
    contractualDocuments: new FormGroup({
      hasEidasCertificate: new FormControl<boolean | null>(null, [Validators.required]),
    }),
  });

  get legalRepGroup(): FormGroup {
    return this.formalForm.get('legalRepresentative') as FormGroup;
  }

  get learGroup(): FormGroup {
    return this.formalForm.get('lear') as FormGroup;
  }

  get contractualGroup(): FormGroup {
    return this.formalForm.get('contractualDocuments') as FormGroup;
  }

  goNext(): void {
    this.currentStep++;
  }

  goBack(): void {
    this.currentStep--;
  }

  onComplete(): void {
    // TODO: Submit formal onboarding data to API
    this.currentStep = 4;
  }
}
