import { HttpErrorResponse } from '@angular/common/http';

export function mapAuthSessionErrorMessage(error: HttpErrorResponse): string {
  if (error.status === 401) {
    const apiMessage = String(error.error?.message ?? '').toLowerCase();

    if (apiMessage.includes('expired')) {
      return 'Η συνεδρία σας έληξε. Συνδεθείτε ξανά.';
    }

    if (apiMessage.includes('revoked')) {
      return 'Η συνεδρία σας έχει ανακληθεί. Συνδεθείτε ξανά.';
    }

    if (apiMessage.includes('invalid')) {
      return 'Μη έγκυρη συνεδρία. Συνδεθείτε ξανά.';
    }

    return 'Δεν είστε συνδεδεμένοι ή η συνεδρία σας έχει λήξει.';
  }

  if (error.status === 429) {
    return 'Πολλές προσπάθειες σε σύντομο χρόνο. Δοκιμάστε ξανά σε λίγο.';
  }

  if (error.status === 403) {
    return 'Δεν έχετε δικαίωμα πρόσβασης σε αυτή τη σελίδα.';
  }

  if (error.status === 0) {
    return 'Δεν ήταν δυνατή η σύνδεση με τον διακομιστή. Ελέγξτε τη σύνδεσή σας.';
  }

  if (error.status >= 500) {
    return 'Σφάλμα διακομιστή. Δοκιμάστε ξανά αργότερα.';
  }

  return 'Προέκυψε σφάλμα πιστοποίησης. Δοκιμάστε ξανά.';
}
