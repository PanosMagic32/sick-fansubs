import { Type } from 'class-transformer';
import { IsDate, IsEnum, IsNumber, IsOptional, IsString, Max, MaxLength, Min } from 'class-validator';

export class SearchDto {
  @IsOptional()
  @IsString()
  @MaxLength(200)
  searchTerm?: string;

  @IsOptional()
  @IsEnum(['blog-post', 'project', 'all'])
  type?: 'blog-post' | 'project' | 'all' = 'all';

  @IsOptional()
  @Type(() => Date)
  @IsDate()
  dateFrom?: Date;

  @IsOptional()
  @Type(() => Date)
  @IsDate()
  dateTo?: Date;

  @Type(() => Number)
  @IsNumber()
  @Min(1)
  @Max(100)
  pageSize = 10;

  @Type(() => Number)
  @IsNumber()
  @Min(0)
  page = 0;
}
