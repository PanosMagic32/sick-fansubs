import { ApiProperty } from '@nestjs/swagger';
import { Type } from 'class-transformer';
import { IsArray, IsDateString, IsOptional, IsString, Matches, MinLength, ValidateNested } from 'class-validator';

const TORRENT_URL_PATTERN = /^https?:\/\//i;
const MAGNET_URL_PATTERN = /^magnet:\?xt=/i;

export class BatchDownloadLinkDto {
  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly name!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  @Matches(TORRENT_URL_PATTERN, {
    message: 'Torrent URL must start with http:// or https://',
  })
  readonly downloadLinkTorrent!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  @Matches(MAGNET_URL_PATTERN, {
    message: 'Magnet URL must start with magnet:?xt=',
  })
  readonly downloadLink!: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  @MinLength(1)
  @Matches(TORRENT_URL_PATTERN, {
    message: '4K torrent URL must start with http:// or https://',
  })
  readonly downloadLink4kTorrent?: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  @MinLength(1)
  @Matches(MAGNET_URL_PATTERN, {
    message: '4K magnet URL must start with magnet:?xt=',
  })
  readonly downloadLink4k?: string;
}

export class CreateProjectDto {
  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly title!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly description!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly thumbnail!: string;

  @ApiProperty({ type: [BatchDownloadLinkDto], required: false })
  @IsOptional()
  @IsArray()
  @ValidateNested({ each: true })
  @Type(() => BatchDownloadLinkDto)
  readonly batchDownloadLinks?: BatchDownloadLinkDto[];

  @ApiProperty({ type: String })
  @IsDateString()
  readonly dateTimeCreated!: string;
}
