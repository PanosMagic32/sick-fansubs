import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import { WebConfigService } from './web-config.service';

export interface MediaUploadResponse {
  url: string;
}

@Injectable({ providedIn: 'root' })
export class MediaService {
  private readonly httpClient = inject(HttpClient);
  private readonly webConfigService = inject(WebConfigService);

  uploadImage(file: File): Observable<MediaUploadResponse> {
    const formData = new FormData();
    formData.append('file', file);

    return this.httpClient.post<MediaUploadResponse>(`${this.webConfigService.API_URL}/media/images`, formData);
  }
}
