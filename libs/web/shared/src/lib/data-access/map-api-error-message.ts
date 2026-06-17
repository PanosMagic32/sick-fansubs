import { HttpErrorResponse } from '@angular/common/http';

/**
 * Maps an HTTP error response to a user-facing Greek message based on status code.
 */
export function mapApiErrorMessage(error: unknown): string {
  if (!(error instanceof HttpErrorResponse)) {
    return 'Προέκυψε άγνωστο σφάλμα.';
  }

  switch (error.status) {
    case 0:
      return 'Αδυναμία σύνδεσης με τον διακομιστή. Ελέγξτε τη σύνδεσή σας.';
    case 400:
      return 'Μη έγκυρο αίτημα. Ελέγξτε τα στοιχεία που υποβάλατε.';
    case 401:
      return 'Δεν είστε συνδεδεμένοι ή η συνεδρία σας έχει λήξει.';
    case 403:
      return 'Δεν έχετε δικαίωμα πρόσβασης σε αυτή την ενέργεια.';
    case 404:
      return 'Το στοιχείο δεν βρέθηκε.';
    case 409:
      return 'Υπάρχει ήδη εγγραφή με αυτά τα στοιχεία.';
    case 429:
      return 'Πολλές προσπάθειες σε σύντομο χρόνο. Δοκιμάστε ξανά σε λίγο.';
    default:
      if (error.status >= 500) {
        return 'Σφάλμα διακομιστή. Δοκιμάστε ξανά αργότερα.';
      }
      return 'Προέκυψε σφάλμα. Δοκιμάστε ξανά.';
  }
}
