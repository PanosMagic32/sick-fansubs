import { ApiProperty } from '@nestjs/swagger';
import { IsDateString, IsOptional, IsString, Matches, MinLength } from 'class-validator';

const URL_PATTERN = /^https?:\/\//i;
const MAGNET_PATTERN = /^magnet:\?xt=/i;

export class CreateBlogPostDto {
  @ApiProperty({ type: String })
  @IsString()
  @MinLength(3)
  readonly title!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(3)
  readonly subtitle!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(10)
  readonly description!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  @Matches(URL_PATTERN, {
    message: 'Thumbnail URL must start with http:// or https://',
  })
  readonly thumbnail!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  @Matches(MAGNET_PATTERN, {
    message: 'Magnet download link must start with magnet:?xt=',
  })
  readonly downloadLink!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  @Matches(URL_PATTERN, {
    message: 'Torrent download link must start with http:// or https://',
  })
  readonly downloadLinkTorrent!: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  @MinLength(1)
  @Matches(MAGNET_PATTERN, {
    message: '4K magnet download link must start with magnet:?xt=',
  })
  readonly downloadLink4k!: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  @MinLength(1)
  @Matches(URL_PATTERN, {
    message: '4K torrent download link must start with http:// or https://',
  })
  readonly downloadLink4kTorrent!: string;

  @ApiProperty({ type: String })
  @IsDateString()
  readonly dateTimeCreated!: string;
}
