import { inject } from '@angular/core';
import { CanDeactivateFn } from '@angular/router';
import { MatDialog } from '@angular/material/dialog';
import { map, of, take } from 'rxjs';

import { ConfirmDialogComponent } from '@web/shared';

import { WebAccountComponent } from '../feature/account/account.component';

export const unsavedChangesGuard: CanDeactivateFn<WebAccountComponent> = (component) => {
  if (!component.isFormDirty()) return of(true);

  const dialog = inject(MatDialog);
  const dialogRef = dialog.open(ConfirmDialogComponent, {
    width: '400px',
    disableClose: true,
    backdropClass: 'darker-backdrop',
    data: {
      title: 'Αλλαγές χωρίς αποθήκευση',
      message: 'Έχετε αλλαγές που δεν έχουν αποθηκευθεί. Θέλετε να φύγετε;',
      confirmLabel: 'Φύγε',
      cancelLabel: 'Ακύρωση',
      isDangerous: true,
    },
  });

  return dialogRef.afterClosed().pipe(
    map((result) => result === true),
    take(1),
  );
};
