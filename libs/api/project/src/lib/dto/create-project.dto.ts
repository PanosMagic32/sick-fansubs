import { ApiProperty } from '@nestjs/swagger';
import { IsArray, IsDateString, IsOptional, IsString, MinLength } from 'class-validator';

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

  @ApiProperty({ type: [String], required: false })
  @IsOptional()
  @IsArray()
  @IsString({ each: true })
  readonly batchDownloadLinks?: string[];

  @ApiProperty({ type: String })
  @IsDateString()
  readonly dateTimeCreated!: string;
}
