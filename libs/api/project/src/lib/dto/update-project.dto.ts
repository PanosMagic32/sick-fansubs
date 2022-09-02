import { ApiProperty, PartialType } from '@nestjs/swagger';

import { CreateProjectDto } from './create-project.dto';

export class UpdateProjectDto extends PartialType(CreateProjectDto) {
  @ApiProperty({ type: String })
  readonly title!: string;

  @ApiProperty({ type: String })
  readonly description!: string;

  @ApiProperty({ type: String })
  readonly thumbnail!: string;

  @ApiProperty({ type: [String] })
  readonly batchDownloadLinks!: string[];

  @ApiProperty({ type: String })
  readonly dateTimeCreated!: string;
}
