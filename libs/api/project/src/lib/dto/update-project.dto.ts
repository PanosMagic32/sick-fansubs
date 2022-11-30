import { ApiProperty, PartialType } from '@nestjs/swagger';

import { CreateProjectDto } from './create-project.dto';

export class UpdateProjectDto extends PartialType(CreateProjectDto) {
    @ApiProperty({ type: String })
    override readonly title!: string;

    @ApiProperty({ type: String })
    override readonly description!: string;

    @ApiProperty({ type: String })
    override readonly thumbnail!: string;

    @ApiProperty({ type: [String] })
    override readonly batchDownloadLinks!: string[];

    @ApiProperty({ type: String })
    override readonly dateTimeCreated!: string;
}
