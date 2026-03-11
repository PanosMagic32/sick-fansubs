import { ApiProperty } from '@nestjs/swagger';
import { IsDateString, IsOptional, IsString, MinLength } from 'class-validator';

export class CreateBlogPostDto {
  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly title!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly subtitle!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly description!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly thumbnail!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly downloadLink!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(1)
  readonly downloadLinkTorrent!: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  readonly downloadLink4k!: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  readonly downloadLink4kTorrent!: string;

  @ApiProperty({ type: String })
  @IsDateString()
  readonly dateTimeCreated!: string;
}
