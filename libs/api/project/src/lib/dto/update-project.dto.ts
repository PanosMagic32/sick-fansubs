import { PartialType } from '@nestjs/swagger';

import { CreateProjectDto } from './create-project.dto';

export class UpdateProjectDto extends PartialType(CreateProjectDto) {
  readonly title!: string;
  readonly description!: string;
  readonly thumbnail!: string;
  readonly batchDownloadLinks!: string[];
  readonly dateTimeCreated!: string;
}
