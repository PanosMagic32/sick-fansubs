import { BadRequestException, Controller, Post, Query, UploadedFile, UseGuards, UseInterceptors } from '@nestjs/common';
import { FileInterceptor } from '@nestjs/platform-express';
import { ApiConsumes, ApiTags } from '@nestjs/swagger';
import { memoryStorage } from 'multer';

import { JwtAuthGuard } from '@api/user';

import { MediaService } from './media.service';

const ALLOWED_MIME_TYPES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp'];
const MAX_FILE_SIZE = 5 * 1024 * 1024; // 5 MB

@ApiTags('Media')
@Controller('media')
export class MediaController {
  constructor(private readonly mediaService: MediaService) {}

  @Post('images')
  @UseGuards(JwtAuthGuard)
  @UseInterceptors(FileInterceptor('file', { storage: memoryStorage() }))
  @ApiConsumes('multipart/form-data')
  async uploadImage(
    @UploadedFile() file: Express.Multer.File,
    @Query('oldImageUrl') oldImageUrl?: string,
  ): Promise<{ url: string }> {
    if (!file) {
      throw new BadRequestException('Δεν παρέχθηκε αρχείο.');
    }

    if (!ALLOWED_MIME_TYPES.includes(file.mimetype)) {
      throw new BadRequestException('Επιτρέπονται μόνο εικόνες jpeg, png, gif και webp.');
    }

    if (file.size > MAX_FILE_SIZE) {
      throw new BadRequestException('Το αρχείο δεν μπορεί να υπερβαίνει τα 5 MB.');
    }

    return this.mediaService.uploadImage(file, oldImageUrl);
  }
}
